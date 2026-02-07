package pm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Jira implements the PM Provider interface using the Jira REST API v3 (Cloud).
// Authentication requires JIRA_URL, JIRA_EMAIL, and JIRA_API_TOKEN environment variables.
type Jira struct {
	*BasePM
}

// NewJira creates a new Jira PM plugin.
func NewJira() *Jira {
	return &Jira{
		BasePM: NewBasePM("jira"),
	}
}

// jiraConfig holds connection settings resolved from environment variables.
type jiraConfig struct {
	baseURL string // e.g. "https://mycompany.atlassian.net"
	auth    string // base64(email:token)
}

func getJiraConfig() (*jiraConfig, error) {
	baseURL := os.Getenv("JIRA_URL")
	if baseURL == "" {
		return nil, fmt.Errorf("JIRA_URL not set")
	}
	email := os.Getenv("JIRA_EMAIL")
	if email == "" {
		return nil, fmt.Errorf("JIRA_EMAIL not set")
	}
	token := os.Getenv("JIRA_API_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("JIRA_API_TOKEN not set")
	}

	baseURL = strings.TrimRight(baseURL, "/")
	auth := base64.StdEncoding.EncodeToString([]byte(email + ":" + token))

	return &jiraConfig{baseURL: baseURL, auth: auth}, nil
}

// jiraRequest performs an authenticated request to the Jira REST API.
func jiraRequest(ctx context.Context, cfg *jiraConfig, method, path string, body io.Reader) ([]byte, error) {
	url := cfg.baseURL + path

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Basic "+cfg.auth)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jira request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading jira response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("jira API error (HTTP %d): %s", resp.StatusCode, string(data))
	}

	return data, nil
}

// Jira REST API response types

type jiraIssue struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Self   string `json:"self"`
	Fields struct {
		Summary     string `json:"summary"`
		Description *struct {
			Content []struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"content"`
		} `json:"description"`
		Status struct {
			Name           string `json:"name"`
			StatusCategory struct {
				Key string `json:"key"`
			} `json:"statusCategory"`
		} `json:"status"`
		IssueType *struct {
			Name string `json:"name"`
		} `json:"issuetype"`
		Parent *struct {
			Key string `json:"key"`
		} `json:"parent"`
		Priority *struct {
			Name string `json:"name"`
		} `json:"priority"`
		Assignee *struct {
			DisplayName string `json:"displayName"`
		} `json:"assignee"`
		Labels  []string `json:"labels"`
		Created string   `json:"created"`
		Updated string   `json:"updated"`
	} `json:"fields"`
}

func (j *Jira) GetIssue(ctx context.Context, issueKey string) (*Issue, error) {
	cfg, err := getJiraConfig()
	if err != nil {
		return nil, err
	}

	data, err := jiraRequest(ctx, cfg, http.MethodGet,
		"/rest/api/3/issue/"+issueKey+"?fields=summary,description,status,issuetype,parent,priority,assignee,labels,created,updated", nil)
	if err != nil {
		return nil, fmt.Errorf("get issue %s: %w", issueKey, err)
	}

	var result jiraIssue
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return jiraIssueToIssue(&result, cfg.baseURL), nil
}

// jiraSearchIssues runs a JQL search and converts results to Issue models.
func jiraSearchIssues(ctx context.Context, cfg *jiraConfig, jql string, fields string, maxResults int) ([]*Issue, error) {
	path := "/rest/api/3/search?jql=" + strings.ReplaceAll(jql, " ", "+") +
		"&fields=" + fields + "&maxResults=" + fmt.Sprintf("%d", maxResults)

	data, err := jiraRequest(ctx, cfg, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Issues []jiraIssue `json:"issues"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	issues := make([]*Issue, len(result.Issues))
	for i := range result.Issues {
		issues[i] = jiraIssueToIssue(&result.Issues[i], cfg.baseURL)
	}
	return issues, nil
}

const jiraDefaultFields = "summary,description,status,issuetype,parent,priority,assignee,labels,created,updated"

func (j *Jira) ListIssues(ctx context.Context, project string, state IssueState) ([]*Issue, error) {
	cfg, err := getJiraConfig()
	if err != nil {
		return nil, err
	}

	jql := fmt.Sprintf("project = %q ORDER BY updated DESC", project)
	if state != "" {
		jqlState := issueStateToJiraCategory(state)
		if jqlState != "" {
			jql = fmt.Sprintf("project = %q AND statusCategory = %q ORDER BY updated DESC", project, jqlState)
		}
	}

	issues, err := jiraSearchIssues(ctx, cfg, jql, jiraDefaultFields, 50)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	return issues, nil
}

func (j *Jira) CreateIssue(ctx context.Context, issue *Issue) (*Issue, error) {
	cfg, err := getJiraConfig()
	if err != nil {
		return nil, err
	}

	// Extract project key from issue.Key (e.g., "ENG" from "ENG-123") or use a provided key
	projectKey := issue.Key
	if projectKey == "" {
		return nil, fmt.Errorf("issue Key must be set to the Jira project key (e.g., \"ENG\")")
	}

	issueTypeName := issueTypeToJiraName(issue.Type)
	payload := map[string]interface{}{
		"fields": map[string]interface{}{
			"project": map[string]string{
				"key": projectKey,
			},
			"summary":   issue.Title,
			"issuetype": map[string]string{"name": issueTypeName},
		},
	}

	if issue.Description != "" {
		payload["fields"].(map[string]interface{})["description"] = map[string]interface{}{
			"type":    "doc",
			"version": 1,
			"content": []map[string]interface{}{
				{
					"type": "paragraph",
					"content": []map[string]interface{}{
						{"type": "text", "text": issue.Description},
					},
				},
			},
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	data, err := jiraRequest(ctx, cfg, http.MethodPost, "/rest/api/3/issue", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create issue: %w", err)
	}

	var result struct {
		ID   string `json:"id"`
		Key  string `json:"key"`
		Self string `json:"self"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	issue.ID = result.ID
	issue.Key = result.Key
	issue.URL = cfg.baseURL + "/browse/" + result.Key
	return issue, nil
}

func (j *Jira) UpdateIssueState(ctx context.Context, issueKey string, state IssueState) error {
	cfg, err := getJiraConfig()
	if err != nil {
		return err
	}

	// Get available transitions for the issue
	data, err := jiraRequest(ctx, cfg, http.MethodGet,
		"/rest/api/3/issue/"+issueKey+"/transitions", nil)
	if err != nil {
		return fmt.Errorf("get transitions for %s: %w", issueKey, err)
	}

	var transitions struct {
		Transitions []struct {
			ID string `json:"id"`
			To struct {
				StatusCategory struct {
					Key string `json:"key"`
				} `json:"statusCategory"`
			} `json:"to"`
			Name string `json:"name"`
		} `json:"transitions"`
	}
	if err := json.Unmarshal(data, &transitions); err != nil {
		return err
	}

	targetCategory := issueStateToJiraCategory(state)
	if targetCategory == "" {
		return fmt.Errorf("cannot map state %q to Jira status category", state)
	}

	// Find matching transition
	var transitionID string
	for _, t := range transitions.Transitions {
		if t.To.StatusCategory.Key == targetCategory {
			transitionID = t.ID
			break
		}
	}
	if transitionID == "" {
		return fmt.Errorf("no available transition to %q for issue %s", state, issueKey)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"transition": map[string]string{"id": transitionID},
	})

	_, err = jiraRequest(ctx, cfg, http.MethodPost,
		"/rest/api/3/issue/"+issueKey+"/transitions", strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("transition issue %s: %w", issueKey, err)
	}

	return nil
}

func (j *Jira) LinkPR(ctx context.Context, issueKey, prURL string) error {
	cfg, err := getJiraConfig()
	if err != nil {
		return err
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"object": map[string]interface{}{
			"url":   prURL,
			"title": "Pull Request",
		},
	})

	_, err = jiraRequest(ctx, cfg, http.MethodPost,
		"/rest/api/3/issue/"+issueKey+"/remotelink", strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("link PR to %s: %w", issueKey, err)
	}

	return nil
}

// jiraIssueToIssue converts a Jira API issue to Athena's Issue model.
func jiraIssueToIssue(ji *jiraIssue, baseURL string) *Issue {
	issue := &Issue{
		ID:        ji.ID,
		Key:       ji.Key,
		Title:     ji.Fields.Summary,
		State:     jiraCategoryToIssueState(ji.Fields.Status.StatusCategory.Key),
		Labels:    ji.Fields.Labels,
		CreatedAt: ji.Fields.Created,
		UpdatedAt: ji.Fields.Updated,
		URL:       baseURL + "/browse/" + ji.Key,
		Type:      IssueTypeUnknown,
	}

	if ji.Fields.IssueType != nil {
		issue.Type = jiraIssueTypeToIssueType(ji.Fields.IssueType.Name)
	}

	if ji.Fields.Parent != nil {
		issue.ParentKey = ji.Fields.Parent.Key
	}

	// Extract plain text from ADF description
	if ji.Fields.Description != nil {
		var parts []string
		for _, block := range ji.Fields.Description.Content {
			for _, inline := range block.Content {
				if inline.Text != "" {
					parts = append(parts, inline.Text)
				}
			}
		}
		issue.Description = strings.Join(parts, "\n")
	}

	if ji.Fields.Priority != nil {
		issue.Priority = jiraPriorityToPriority(ji.Fields.Priority.Name)
	}

	if ji.Fields.Assignee != nil {
		issue.Assignee = ji.Fields.Assignee.DisplayName
	}

	return issue
}

// Jira status categories: "new", "indeterminate", "done", "undefined"
func jiraCategoryToIssueState(category string) IssueState {
	switch strings.ToLower(category) {
	case "new":
		return IssueStateTodo
	case "indeterminate":
		return IssueStateInProgress
	case "done":
		return IssueStateDone
	default:
		return IssueStateTodo
	}
}

func issueStateToJiraCategory(state IssueState) string {
	switch state {
	case IssueStateBacklog, IssueStateTodo:
		return "new"
	case IssueStateInProgress:
		return "indeterminate"
	case IssueStateDone:
		return "done"
	case IssueStateCanceled:
		return "done"
	default:
		return ""
	}
}

// GetEpic fetches a Jira issue by key, verifies it's an Epic, and populates its children.
func (j *Jira) GetEpic(ctx context.Context, epicKey string) (*Issue, error) {
	issue, err := j.GetIssue(ctx, epicKey)
	if err != nil {
		return nil, err
	}
	if issue.Type != IssueTypeEpic {
		return nil, fmt.Errorf("issue %s is a %s, not an epic", epicKey, issue.Type)
	}

	// Fetch child issues via JQL
	cfg, err := getJiraConfig()
	if err != nil {
		return nil, err
	}

	jql := fmt.Sprintf(`"Epic Link" = %s ORDER BY created ASC`, epicKey)
	children, err := jiraSearchIssues(ctx, cfg, jql, "summary,status,issuetype,parent", 100)
	if err != nil {
		return nil, fmt.Errorf("list epic children for %s: %w", epicKey, err)
	}

	issue.Children = make([]string, len(children))
	for i, child := range children {
		issue.Children[i] = child.Key
	}

	return issue, nil
}

// ListEpics returns all epics in a Jira project.
func (j *Jira) ListEpics(ctx context.Context, project string) ([]*Issue, error) {
	cfg, err := getJiraConfig()
	if err != nil {
		return nil, err
	}

	jql := fmt.Sprintf("project = %q AND issuetype = Epic ORDER BY updated DESC", project)
	issues, err := jiraSearchIssues(ctx, cfg, jql, jiraDefaultFields, 50)
	if err != nil {
		return nil, fmt.Errorf("list epics: %w", err)
	}
	return issues, nil
}

// jiraIssueTypeToIssueType maps Jira issue type names to IssueType.
func jiraIssueTypeToIssueType(name string) IssueType {
	switch strings.ToLower(name) {
	case "epic":
		return IssueTypeEpic
	case "story":
		return IssueTypeStory
	case "task", "sub-task", "subtask":
		return IssueTypeTask
	case "bug":
		return IssueTypeBug
	default:
		return IssueTypeUnknown
	}
}

// issueTypeToJiraName maps IssueType to a Jira issue type name for creation.
func issueTypeToJiraName(t IssueType) string {
	switch t {
	case IssueTypeEpic:
		return "Epic"
	case IssueTypeStory:
		return "Story"
	case IssueTypeBug:
		return "Bug"
	case IssueTypeTask:
		return "Task"
	default:
		return "Task"
	}
}

func jiraPriorityToPriority(name string) Priority {
	switch strings.ToLower(name) {
	case "highest", "blocker":
		return PriorityUrgent
	case "high", "critical":
		return PriorityHigh
	case "medium":
		return PriorityMedium
	case "low":
		return PriorityLow
	case "lowest":
		return PriorityLow
	default:
		return PriorityNone
	}
}
