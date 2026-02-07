// Package pm provides Project Management plugins.
package pm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const linearGraphQLEndpoint = "https://api.linear.app/graphql"

// Linear implements the PM Provider interface using the Linear GraphQL API.
type Linear struct {
	*BasePM
	apiKey     string
	httpClient *http.Client
}

// NewLinear creates a new Linear PM plugin.
// Returns an error if apiKey is empty — there is no stub/fallback mode.
func NewLinear(apiKey string) (*Linear, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("linear: LINEAR_API_KEY is required (no stub mode)")
	}
	return &Linear{
		BasePM:     NewBasePM("linear"),
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}, nil
}

// graphQLRequest is the body sent to Linear's GraphQL endpoint.
type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// graphQLResponse is a generic envelope for Linear GraphQL responses.
type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// do executes a GraphQL request against Linear and returns the raw data payload.
func (l *Linear) do(ctx context.Context, req graphQLRequest) (json.RawMessage, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("linear: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", linearGraphQLEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("linear: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", l.apiKey)

	resp, err := l.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("linear: execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("linear: unexpected status %d", resp.StatusCode)
	}

	var gqlResp graphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		return nil, fmt.Errorf("linear: decode response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("linear: API error: %s", gqlResp.Errors[0].Message)
	}
	return gqlResp.Data, nil
}

// ---------- issue response types ----------

type linearIssueNode struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    int    `json:"priority"`
	URL         string `json:"url"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	State       *struct {
		Name string `json:"name"`
	} `json:"state"`
	Assignee *struct {
		Name string `json:"name"`
	} `json:"assignee"`
	Labels struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	Team *struct {
		Key string `json:"key"`
	} `json:"team"`
	Parent *struct {
		ID         string `json:"id"`
		Identifier string `json:"identifier"`
	} `json:"parent"`
	Project *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"project"`
	Children struct {
		Nodes []struct {
			Identifier string `json:"identifier"`
		} `json:"nodes"`
	} `json:"children"`
}

func (n *linearIssueNode) toIssue() *Issue {
	issue := &Issue{
		ID:          n.ID,
		Key:         n.Identifier,
		Title:       n.Title,
		Description: n.Description,
		Priority:    Priority(n.Priority),
		URL:         n.URL,
		CreatedAt:   n.CreatedAt,
		UpdatedAt:   n.UpdatedAt,
		Type:        IssueTypeStory, // default: Linear issues map to stories
	}
	if n.State != nil {
		issue.State = linearStateToIssueState(n.State.Name)
	}
	if n.Assignee != nil {
		issue.Assignee = n.Assignee.Name
	}
	labels := make([]string, 0, len(n.Labels.Nodes))
	for _, lbl := range n.Labels.Nodes {
		labels = append(labels, lbl.Name)
	}
	issue.Labels = labels

	// Hierarchy: sub-issues have a parent, top-level issues are stories
	if n.Parent != nil {
		issue.ParentKey = n.Parent.Identifier
		issue.Type = IssueTypeTask // child issues are tasks
	}

	// Collect child identifiers
	if len(n.Children.Nodes) > 0 {
		issue.Children = make([]string, len(n.Children.Nodes))
		for i, child := range n.Children.Nodes {
			issue.Children[i] = child.Identifier
		}
	}

	return issue
}

// issueFields is the shared GraphQL fragment for issue queries.
const issueFields = `
	id
	identifier
	title
	description
	priority
	url
	createdAt
	updatedAt
	state { name }
	assignee { name }
	labels { nodes { name } }
	team { key }
	parent { id identifier }
	project { id name }
	children { nodes { identifier } }
`

func (l *Linear) GetIssue(ctx context.Context, issueKey string) (*Issue, error) {
	data, err := l.do(ctx, graphQLRequest{
		Query: `query($id: String!) {
			issue(id: $id) {` + issueFields + `}
		}`,
		Variables: map[string]any{"id": issueKey},
	})
	if err != nil {
		return nil, err
	}

	var result struct {
		Issue *linearIssueNode `json:"issue"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("linear: decode issue: %w", err)
	}
	if result.Issue == nil {
		return nil, fmt.Errorf("linear: issue not found: %s", issueKey)
	}
	return result.Issue.toIssue(), nil
}

func (l *Linear) ListIssues(ctx context.Context, project string, state IssueState) ([]*Issue, error) {
	// Build a filter object for the issues query.
	// "project" maps to a Linear team key.
	filter := map[string]any{
		"team": map[string]any{"key": map[string]any{"eq": project}},
	}
	if state != "" {
		filter["state"] = map[string]any{"name": map[string]any{"eqFold": issueStateToLinearState(state)}}
	}

	data, err := l.do(ctx, graphQLRequest{
		Query: `query($filter: IssueFilter) {
			issues(filter: $filter, first: 100) {
				nodes {` + issueFields + `}
			}
		}`,
		Variables: map[string]any{"filter": filter},
	})
	if err != nil {
		return nil, err
	}

	var result struct {
		Issues struct {
			Nodes []linearIssueNode `json:"nodes"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("linear: decode issues: %w", err)
	}

	issues := make([]*Issue, len(result.Issues.Nodes))
	for i := range result.Issues.Nodes {
		issues[i] = result.Issues.Nodes[i].toIssue()
	}
	return issues, nil
}

func (l *Linear) CreateIssue(ctx context.Context, issue *Issue) (*Issue, error) {
	// We need a team ID. If the caller set Labels[0] as team key, or we look
	// up via the issue Key prefix, we need to resolve. For simplicity the
	// caller should pass the team key as the first label or we derive from Key.
	teamKey := ""
	if issue.Key != "" {
		if prefix, _ := splitIssueKey(issue.Key); prefix != "" {
			teamKey = prefix
		}
	}

	// Resolve team ID from team key if available.
	var teamID string
	if teamKey != "" {
		tid, err := l.resolveTeamID(ctx, teamKey)
		if err != nil {
			return nil, fmt.Errorf("linear: resolve team for create: %w", err)
		}
		teamID = tid
	} else {
		return nil, fmt.Errorf("linear: team key is required to create an issue (set issue.Key prefix, e.g. \"ENG-\")")
	}

	input := map[string]any{
		"title":  issue.Title,
		"teamId": teamID,
	}
	if issue.Description != "" {
		input["description"] = issue.Description
	}
	if issue.Priority != PriorityNone {
		input["priority"] = int(issue.Priority)
	}

	data, err := l.do(ctx, graphQLRequest{
		Query: `mutation($input: IssueCreateInput!) {
			issueCreate(input: $input) {
				success
				issue {` + issueFields + `}
			}
		}`,
		Variables: map[string]any{"input": input},
	})
	if err != nil {
		return nil, err
	}

	var result struct {
		IssueCreate struct {
			Success bool             `json:"success"`
			Issue   *linearIssueNode `json:"issue"`
		} `json:"issueCreate"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("linear: decode create response: %w", err)
	}
	if !result.IssueCreate.Success || result.IssueCreate.Issue == nil {
		return nil, fmt.Errorf("linear: issue creation failed")
	}
	return result.IssueCreate.Issue.toIssue(), nil
}

func (l *Linear) UpdateIssueState(ctx context.Context, issueKey string, state IssueState) error {
	// First resolve the workflow state ID for the target state name.
	// We look up the issue to get its team, then find the matching state.
	issue, err := l.GetIssue(ctx, issueKey)
	if err != nil {
		return fmt.Errorf("linear: fetch issue for state update: %w", err)
	}

	// Get the team key from the issue identifier prefix.
	teamKey, _ := splitIssueKey(issue.Key)
	if teamKey == "" {
		return fmt.Errorf("linear: cannot determine team from issue key %q", issue.Key)
	}

	stateID, err := l.resolveStateID(ctx, teamKey, issueStateToLinearState(state))
	if err != nil {
		return err
	}

	data, err := l.do(ctx, graphQLRequest{
		Query: `mutation($id: String!, $input: IssueUpdateInput!) {
			issueUpdate(id: $id, input: $input) {
				success
			}
		}`,
		Variables: map[string]any{
			"id":    issue.ID,
			"input": map[string]any{"stateId": stateID},
		},
	})
	if err != nil {
		return err
	}

	var result struct {
		IssueUpdate struct {
			Success bool `json:"success"`
		} `json:"issueUpdate"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("linear: decode update response: %w", err)
	}
	if !result.IssueUpdate.Success {
		return fmt.Errorf("linear: state update failed for %s", issueKey)
	}
	return nil
}

// linearProjectNode maps a Linear Project to an Issue with IssueTypeEpic.
type linearProjectNode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
	State       string `json:"state"`
	StartDate   string `json:"startDate"`
	TargetDate  string `json:"targetDate"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	Issues      struct {
		Nodes []struct {
			Identifier string `json:"identifier"`
		} `json:"nodes"`
	} `json:"issues"`
}

func (p *linearProjectNode) toIssue() *Issue {
	issue := &Issue{
		ID:          p.ID,
		Key:         p.ID, // Linear projects use UUID, not identifiers like ENG-123
		Title:       p.Name,
		Description: p.Description,
		URL:         p.URL,
		Type:        IssueTypeEpic,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
	if len(p.Issues.Nodes) > 0 {
		issue.Children = make([]string, len(p.Issues.Nodes))
		for i, child := range p.Issues.Nodes {
			issue.Children[i] = child.Identifier
		}
	}
	return issue
}

const projectFields = `
	id
	name
	description
	url
	state
	startDate
	targetDate
	createdAt
	updatedAt
	issues { nodes { identifier } }
`

func (l *Linear) GetEpic(ctx context.Context, epicKey string) (*Issue, error) {
	data, err := l.do(ctx, graphQLRequest{
		Query: `query($id: String!) {
			project(id: $id) {` + projectFields + `}
		}`,
		Variables: map[string]any{"id": epicKey},
	})
	if err != nil {
		return nil, err
	}

	var result struct {
		Project *linearProjectNode `json:"project"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("linear: decode project: %w", err)
	}
	if result.Project == nil {
		return nil, fmt.Errorf("linear: project not found: %s", epicKey)
	}
	return result.Project.toIssue(), nil
}

func (l *Linear) ListEpics(ctx context.Context, project string) ([]*Issue, error) {
	// Resolve team ID from team key to filter projects by team.
	teamID, err := l.resolveTeamID(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("linear: resolve team for epics: %w", err)
	}

	data, err := l.do(ctx, graphQLRequest{
		Query: `query($filter: ProjectFilter) {
			projects(filter: $filter, first: 50) {
				nodes {` + projectFields + `}
			}
		}`,
		Variables: map[string]any{
			"filter": map[string]any{
				"accessibleTeams": map[string]any{
					"id": map[string]any{"eq": teamID},
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	var result struct {
		Projects struct {
			Nodes []linearProjectNode `json:"nodes"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("linear: decode projects: %w", err)
	}

	issues := make([]*Issue, len(result.Projects.Nodes))
	for i := range result.Projects.Nodes {
		issues[i] = result.Projects.Nodes[i].toIssue()
	}
	return issues, nil
}

func (l *Linear) LinkPR(ctx context.Context, issueKey, prURL string) error {
	// Linear auto-links PRs when the branch name or PR description contains
	// the issue identifier — this is effectively a no-op.
	return nil
}

// ---------- helpers ----------

// resolveTeamID looks up the Linear team UUID from a team key (e.g. "ENG").
func (l *Linear) resolveTeamID(ctx context.Context, teamKey string) (string, error) {
	data, err := l.do(ctx, graphQLRequest{
		Query: `query($filter: TeamFilter) {
			teams(filter: $filter, first: 1) {
				nodes { id key }
			}
		}`,
		Variables: map[string]any{
			"filter": map[string]any{"key": map[string]any{"eq": teamKey}},
		},
	})
	if err != nil {
		return "", err
	}

	var result struct {
		Teams struct {
			Nodes []struct {
				ID  string `json:"id"`
				Key string `json:"key"`
			} `json:"nodes"`
		} `json:"teams"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("linear: decode teams: %w", err)
	}
	if len(result.Teams.Nodes) == 0 {
		return "", fmt.Errorf("linear: team not found: %s", teamKey)
	}
	return result.Teams.Nodes[0].ID, nil
}

// resolveStateID finds a workflow state ID by name for a given team.
func (l *Linear) resolveStateID(ctx context.Context, teamKey, stateName string) (string, error) {
	data, err := l.do(ctx, graphQLRequest{
		Query: `query($filter: WorkflowStateFilter) {
			workflowStates(filter: $filter, first: 50) {
				nodes { id name }
			}
		}`,
		Variables: map[string]any{
			"filter": map[string]any{
				"team": map[string]any{"key": map[string]any{"eq": teamKey}},
				"name": map[string]any{"eqFold": stateName},
			},
		},
	})
	if err != nil {
		return "", err
	}

	var result struct {
		WorkflowStates struct {
			Nodes []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"nodes"`
		} `json:"workflowStates"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("linear: decode workflow states: %w", err)
	}
	if len(result.WorkflowStates.Nodes) == 0 {
		return "", fmt.Errorf("linear: workflow state %q not found for team %s", stateName, teamKey)
	}
	return result.WorkflowStates.Nodes[0].ID, nil
}

// splitIssueKey splits "ENG-123" into ("ENG", "123").
func splitIssueKey(key string) (prefix, number string) {
	idx := strings.LastIndex(key, "-")
	if idx < 0 {
		return key, ""
	}
	return key[:idx], key[idx+1:]
}

// ---------- state mapping ----------

func linearStateToIssueState(state string) IssueState {
	switch strings.ToLower(state) {
	case "backlog":
		return IssueStateBacklog
	case "todo", "unstarted":
		return IssueStateTodo
	case "in progress", "started":
		return IssueStateInProgress
	case "done", "completed":
		return IssueStateDone
	case "canceled", "cancelled":
		return IssueStateCanceled
	default:
		return IssueStateTodo
	}
}

func issueStateToLinearState(state IssueState) string {
	switch state {
	case IssueStateBacklog:
		return "Backlog"
	case IssueStateTodo:
		return "Todo"
	case IssueStateInProgress:
		return "In Progress"
	case IssueStateDone:
		return "Done"
	case IssueStateCanceled:
		return "Canceled"
	default:
		return "Todo"
	}
}
