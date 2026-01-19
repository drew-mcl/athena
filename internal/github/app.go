// Package github provides GitHub App authentication and API operations.
// This enables Athena agents to create PRs under bot identities.
package github

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/drewfead/athena/internal/config"
	"github.com/drewfead/athena/internal/logging"
	"github.com/golang-jwt/jwt/v5"
)

// AppClient handles GitHub App authentication and API operations.
type AppClient struct {
	identity *config.AgentIdentity

	// Cached installation token
	mu          sync.RWMutex
	token       string
	tokenExpiry time.Time
}

// NewAppClient creates a GitHub App client for the given identity.
// Returns nil if the identity doesn't have GitHub App credentials.
func NewAppClient(identity *config.AgentIdentity) *AppClient {
	if identity == nil || !identity.HasGitHubApp() {
		return nil
	}
	return &AppClient{identity: identity}
}

// PROptions configures a pull request creation.
type PROptions struct {
	Owner string // Repository owner
	Repo  string // Repository name
	Title string // PR title
	Body  string // PR body (markdown)
	Head  string // Source branch
	Base  string // Target branch (e.g., "main")
	Draft bool   // Create as draft PR
}

// PRResult contains the result of creating a PR.
type PRResult struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	NodeID  string `json:"node_id"`
}

// CreatePR creates a pull request using the GitHub App identity.
func (c *AppClient) CreatePR(opts PROptions) (*PRResult, error) {
	token, err := c.getInstallationToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get installation token: %w", err)
	}

	// Build request body
	body := map[string]any{
		"title": opts.Title,
		"body":  opts.Body,
		"head":  opts.Head,
		"base":  opts.Base,
	}
	if opts.Draft {
		body["draft"] = true
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create PR via GitHub API
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls", opts.Owner, opts.Repo)
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("GitHub API error: %d - %s", resp.StatusCode, string(respBody))
	}

	var result PRResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// getInstallationToken returns a valid installation access token,
// refreshing it if necessary.
func (c *AppClient) getInstallationToken() (string, error) {
	c.mu.RLock()
	if c.token != "" && time.Now().Before(c.tokenExpiry.Add(-5*time.Minute)) {
		token := c.token
		c.mu.RUnlock()
		return token, nil
	}
	c.mu.RUnlock()

	// Need to refresh token
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.token != "" && time.Now().Before(c.tokenExpiry.Add(-5*time.Minute)) {
		return c.token, nil
	}

	// Generate JWT
	jwtToken, err := c.generateJWT()
	if err != nil {
		return "", fmt.Errorf("failed to generate JWT: %w", err)
	}

	// Exchange JWT for installation token
	token, expiry, err := c.exchangeForInstallationToken(jwtToken)
	if err != nil {
		return "", err
	}

	c.token = token
	c.tokenExpiry = expiry

	logging.Debug("refreshed GitHub App installation token",
		"app_id", c.identity.GitHubAppID,
		"expires", expiry.Format(time.RFC3339))

	return token, nil
}

// generateJWT creates a signed JWT for GitHub App authentication.
func (c *AppClient) generateJWT() (string, error) {
	// Load private key
	keyData, err := os.ReadFile(c.identity.PrivateKeyPath)
	if err != nil {
		return "", fmt.Errorf("failed to read private key: %w", err)
	}

	privateKey, err := parseRSAPrivateKey(keyData)
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %w", err)
	}

	// Create JWT claims
	now := time.Now()
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)), // Clock skew buffer
		ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),  // Max 10 minutes
		Issuer:    c.identity.GitHubAppID,
	}

	// Sign the token
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signedToken, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}

	return signedToken, nil
}

// exchangeForInstallationToken exchanges a JWT for an installation access token.
func (c *AppClient) exchangeForInstallationToken(jwtToken string) (string, time.Time, error) {
	url := fmt.Sprintf("https://api.github.com/app/installations/%s/access_tokens", c.identity.InstallationID)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		return "", time.Time{}, fmt.Errorf("failed to get installation token: %d - %s", resp.StatusCode, string(body))
	}

	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", time.Time{}, fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Token, result.ExpiresAt, nil
}

// parseRSAPrivateKey parses a PEM-encoded RSA private key.
func parseRSAPrivateKey(pemData []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	// Try PKCS#1 format first (RSA PRIVATE KEY)
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	// Try PKCS#8 format (PRIVATE KEY)
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not RSA")
	}

	return rsaKey, nil
}

// ParseRepoFromRemote extracts owner/repo from a git remote URL.
// Supports both HTTPS and SSH formats.
func ParseRepoFromRemote(remoteURL string) (owner, repo string, err error) {
	// SSH format: git@github.com:owner/repo.git
	if strings.HasPrefix(remoteURL, "git@github.com:") {
		path := strings.TrimPrefix(remoteURL, "git@github.com:")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.Split(path, "/")
		if len(parts) != 2 {
			return "", "", fmt.Errorf("invalid SSH remote URL: %s", remoteURL)
		}
		return parts[0], parts[1], nil
	}

	// HTTPS format: https://github.com/owner/repo.git
	if strings.HasPrefix(remoteURL, "https://github.com/") {
		path := strings.TrimPrefix(remoteURL, "https://github.com/")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.Split(path, "/")
		if len(parts) != 2 {
			return "", "", fmt.Errorf("invalid HTTPS remote URL: %s", remoteURL)
		}
		return parts[0], parts[1], nil
	}

	return "", "", fmt.Errorf("unsupported remote URL format: %s", remoteURL)
}

// GetInstallationID fetches the installation ID for a specific repository.
// This is useful when the installation ID isn't pre-configured.
func (c *AppClient) GetInstallationID(owner, repo string) (int64, error) {
	jwtToken, err := c.generateJWT()
	if err != nil {
		return 0, fmt.Errorf("failed to generate JWT: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/installation", owner, repo)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("failed to get installation: %d - %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("failed to parse response: %w", err)
	}

	return result.ID, nil
}

// IdentityForArchetype returns the appropriate GitHub App client for an archetype.
// Uses the ata-{harness} naming convention where executor uses ata-clc (Claude Code)
// and other archetypes use ata-codex (Codex) by default.
func IdentityForArchetype(cfg *config.Config, archetype string) *AppClient {
	identities := cfg.Integrations.Identities

	// Check archetype-specific identity
	if identities.Archetypes != nil {
		if identity := identities.Archetypes[archetype]; identity != nil && identity.HasGitHubApp() {
			return NewAppClient(identity)
		}
	}

	// Fall back to default
	if identities.Default != nil && identities.Default.HasGitHubApp() {
		return NewAppClient(identities.Default)
	}

	return nil
}

// String returns the identity name for logging.
func (c *AppClient) String() string {
	if c == nil || c.identity == nil {
		return "<no identity>"
	}
	return c.identity.Name
}

// InstallationID returns the configured installation ID as an int64.
func (c *AppClient) InstallationID() (int64, error) {
	return strconv.ParseInt(c.identity.InstallationID, 10, 64)
}
