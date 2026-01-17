// Package ticket provides interfaces for ticket system integration.
package ticket

import "context"

// Ticket represents a ticket from an issue tracking system.
type Ticket struct {
	ID          string // e.g., "ENG-123"
	Summary     string // Short title/summary
	Description string // Full description (may be markdown)
	URL         string // Link to the ticket
	Status      string // e.g., "in progress", "todo"
	Assignee    string // Username/email of assignee
	Project     string // Project name
}

// Client defines the interface for fetching tickets.
type Client interface {
	// GetTicket retrieves a ticket by its ID.
	GetTicket(ctx context.Context, id string) (*Ticket, error)

	// Name returns the name of the ticket system (e.g., "Linear", "Jira").
	Name() string
}

// ParseTicketID extracts the prefix and number from a ticket ID.
// e.g., "ENG-123" -> ("ENG", "123")
func ParseTicketID(id string) (prefix, number string) {
	for i, c := range id {
		if c == '-' {
			return id[:i], id[i+1:]
		}
	}
	return id, ""
}
