package ticket

import (
	"context"
	"fmt"
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

// GetTicket retrieves a ticket from Linear.
// Currently returns a stub response - will be extended with actual API calls.
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

	// TODO: Implement actual Linear API integration
	// GraphQL endpoint: https://api.linear.app/graphql
	// Query:
	// query GetIssue($id: String!) {
	//   issue(id: $id) {
	//     id
	//     identifier
	//     title
	//     description
	//     url
	//     state { name }
	//     assignee { name email }
	//     project { name }
	//   }
	// }

	return &Ticket{
		ID:      id,
		Summary: fmt.Sprintf("Ticket %s", id),
		URL:     fmt.Sprintf("https://linear.app/issue/%s", id),
	}, nil
}
