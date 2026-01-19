package ticket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// LinearClient implements Client for Linear.
// This is a stub that will be extended with actual Linear API integration.
type LinearClient struct {
	apiKey string
	teamID string
}

// NewLinearClient creates a new Linear client.
// apiKey can be empty for stub mode.
func NewLinearClient(apiKey, teamID string) *LinearClient {
	return &LinearClient{
		apiKey: apiKey,
		teamID: teamID,
	}
}

// Name returns "Linear".
func (c *LinearClient) Name() string {
	return "Linear"
}

// linearQuery represents a GraphQL query request.
type linearQuery struct {
	Query string `json:"query"`
}

// linearResponse represents a GraphQL response from Linear.
type linearResponse struct {
	Data struct {
		Issue *linearIssue `json:"issue"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// linearIssue represents an issue from Linear's GraphQL API.
type linearIssue struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	State       struct {
		Name string `json:"name"`
	} `json:"state"`
	Assignee *struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"assignee"`
	Project *struct {
		Name string `json:"name"`
	} `json:"project"`
}

// GetTicket retrieves a ticket from Linear.
func (c *LinearClient) GetTicket(ctx context.Context, id string) (*Ticket, error) {
	if c.apiKey == "" {
		// Stub mode - return a placeholder ticket
		return &Ticket{
			ID:          id,
			Summary:     fmt.Sprintf("Ticket %s", id),
			Description: "Ticket details will be fetched from Linear when API key is configured.",
			URL:         fmt.Sprintf("https://linear.app/issue/%s", id),
			Status:      "unknown",
		}, nil
	}

	// Build GraphQL query
	query := linearQuery{
		Query: fmt.Sprintf(`query GetIssue($id: String!) {
			issue(id: $id) {
				id
				identifier
				title
				description
				url
				state { name }
				assignee { name email }
				project { name }
			}
		}`),
	}

	// Marshal query
	reqBody, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("marshal query: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.linear.app/graphql", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.apiKey)

	// Execute request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	// Parse response
	var respData linearResponse
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Check for errors
	if len(respData.Errors) > 0 {
		return nil, fmt.Errorf("linear API error: %s", respData.Errors[0].Message)
	}

	// Check if issue found
	if respData.Data.Issue == nil {
		return nil, fmt.Errorf("ticket not found: %s", id)
	}

	// Convert to Ticket struct
	issue := respData.Data.Issue
	ticket := &Ticket{
		ID:          issue.ID,
		Summary:     issue.Title,
		Description: issue.Description,
		URL:         issue.URL,
		Status:      issue.State.Name,
	}

	// Add assignee info if available
	if issue.Assignee != nil {
		ticket.Assignee = fmt.Sprintf("%s <%s>", issue.Assignee.Name, issue.Assignee.Email)
	}

	// Add project info if available
	if issue.Project != nil {
		ticket.Project = issue.Project.Name
	}

	return ticket, nil
}
