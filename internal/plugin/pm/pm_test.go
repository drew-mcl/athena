package pm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestNewJira(t *testing.T) {
	j := NewJira()
	if j.Name() != "jira" {
		t.Errorf("Name() = %q, want %q", j.Name(), "jira")
	}
	if j.Category() != "pm" {
		t.Errorf("Category() = %q, want %q", j.Category(), "pm")
	}
}

func TestNewLinear(t *testing.T) {
	t.Run("empty api key returns error", func(t *testing.T) {
		_, err := NewLinear("")
		if err == nil {
			t.Fatal("expected error for empty API key")
		}
	})

	t.Run("valid api key", func(t *testing.T) {
		l, err := NewLinear("lin_test_key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if l.Name() != "linear" {
			t.Errorf("Name() = %q, want %q", l.Name(), "linear")
		}
		if l.Category() != "pm" {
			t.Errorf("Category() = %q, want %q", l.Category(), "pm")
		}
	})
}

func TestGetJiraConfig(t *testing.T) {
	// Save and clear env vars
	origURL := os.Getenv("JIRA_URL")
	origEmail := os.Getenv("JIRA_EMAIL")
	origToken := os.Getenv("JIRA_API_TOKEN")
	defer func() {
		setOrUnset("JIRA_URL", origURL)
		setOrUnset("JIRA_EMAIL", origEmail)
		setOrUnset("JIRA_API_TOKEN", origToken)
	}()

	tests := []struct {
		name      string
		url       string
		email     string
		token     string
		wantErr   bool
		errSubstr string
	}{
		{"all missing", "", "", "", true, "JIRA_URL"},
		{"missing email", "https://test.atlassian.net", "", "", true, "JIRA_EMAIL"},
		{"missing token", "https://test.atlassian.net", "user@test.com", "", true, "JIRA_API_TOKEN"},
		{"all present", "https://test.atlassian.net/", "user@test.com", "tok123", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setOrUnset("JIRA_URL", tt.url)
			setOrUnset("JIRA_EMAIL", tt.email)
			setOrUnset("JIRA_API_TOKEN", tt.token)

			cfg, err := getJiraConfig()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// URL should have trailing slash trimmed
			if cfg.baseURL != "https://test.atlassian.net" {
				t.Errorf("baseURL = %q, want %q", cfg.baseURL, "https://test.atlassian.net")
			}
			if cfg.auth == "" {
				t.Error("auth should not be empty")
			}
		})
	}
}

func setOrUnset(key, value string) {
	if value == "" {
		os.Unsetenv(key)
	} else {
		os.Setenv(key, value)
	}
}

func TestJiraCategoryToIssueState(t *testing.T) {
	tests := []struct {
		category string
		want     IssueState
	}{
		{"new", IssueStateTodo},
		{"New", IssueStateTodo},
		{"indeterminate", IssueStateInProgress},
		{"done", IssueStateDone},
		{"unknown", IssueStateTodo},
	}

	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			got := jiraCategoryToIssueState(tt.category)
			if got != tt.want {
				t.Errorf("jiraCategoryToIssueState(%q) = %q, want %q", tt.category, got, tt.want)
			}
		})
	}
}

func TestIssueStateToJiraCategory(t *testing.T) {
	tests := []struct {
		state IssueState
		want  string
	}{
		{IssueStateBacklog, "new"},
		{IssueStateTodo, "new"},
		{IssueStateInProgress, "indeterminate"},
		{IssueStateDone, "done"},
		{IssueStateCanceled, "done"},
		{IssueState("unknown"), ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			got := issueStateToJiraCategory(tt.state)
			if got != tt.want {
				t.Errorf("issueStateToJiraCategory(%q) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

func TestJiraPriorityToPriority(t *testing.T) {
	tests := []struct {
		name string
		want Priority
	}{
		{"Highest", PriorityUrgent},
		{"Blocker", PriorityUrgent},
		{"High", PriorityHigh},
		{"Critical", PriorityHigh},
		{"Medium", PriorityMedium},
		{"Low", PriorityLow},
		{"Lowest", PriorityLow},
		{"Unknown", PriorityNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jiraPriorityToPriority(tt.name)
			if got != tt.want {
				t.Errorf("jiraPriorityToPriority(%q) = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

func TestJiraIssueToIssue(t *testing.T) {
	ji := &jiraIssue{
		ID:  "10001",
		Key: "ENG-123",
	}
	ji.Fields.Summary = "Test issue"
	ji.Fields.Status.Name = "In Progress"
	ji.Fields.Status.StatusCategory.Key = "indeterminate"
	ji.Fields.Labels = []string{"bug", "urgent"}
	ji.Fields.Created = "2024-01-01T00:00:00Z"
	ji.Fields.Updated = "2024-01-02T00:00:00Z"
	ji.Fields.Priority = &struct {
		Name string `json:"name"`
	}{Name: "High"}
	ji.Fields.Assignee = &struct {
		DisplayName string `json:"displayName"`
	}{DisplayName: "Alice"}
	ji.Fields.IssueType = &struct {
		Name string `json:"name"`
	}{Name: "Story"}
	ji.Fields.Parent = &struct {
		Key string `json:"key"`
	}{Key: "ENG-100"}

	issue := jiraIssueToIssue(ji, "https://test.atlassian.net")

	if issue.ID != "10001" {
		t.Errorf("ID = %q, want %q", issue.ID, "10001")
	}
	if issue.Key != "ENG-123" {
		t.Errorf("Key = %q, want %q", issue.Key, "ENG-123")
	}
	if issue.Title != "Test issue" {
		t.Errorf("Title = %q, want %q", issue.Title, "Test issue")
	}
	if issue.State != IssueStateInProgress {
		t.Errorf("State = %q, want %q", issue.State, IssueStateInProgress)
	}
	if issue.Priority != PriorityHigh {
		t.Errorf("Priority = %d, want %d", issue.Priority, PriorityHigh)
	}
	if issue.Assignee != "Alice" {
		t.Errorf("Assignee = %q, want %q", issue.Assignee, "Alice")
	}
	if issue.URL != "https://test.atlassian.net/browse/ENG-123" {
		t.Errorf("URL = %q", issue.URL)
	}
	if len(issue.Labels) != 2 {
		t.Errorf("Labels count = %d, want 2", len(issue.Labels))
	}
	if issue.Type != IssueTypeStory {
		t.Errorf("Type = %q, want %q", issue.Type, IssueTypeStory)
	}
	if issue.ParentKey != "ENG-100" {
		t.Errorf("ParentKey = %q, want %q", issue.ParentKey, "ENG-100")
	}
}

func TestJiraIssueToIssueWithDescription(t *testing.T) {
	ji := &jiraIssue{
		ID:  "10001",
		Key: "ENG-456",
	}
	ji.Fields.Summary = "Issue with description"
	ji.Fields.Status.StatusCategory.Key = "new"
	ji.Fields.Description = &struct {
		Content []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"content"`
	}{
		Content: []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}{
			{Content: []struct {
				Text string `json:"text"`
			}{{Text: "First paragraph"}}},
			{Content: []struct {
				Text string `json:"text"`
			}{{Text: "Second paragraph"}}},
		},
	}

	issue := jiraIssueToIssue(ji, "https://test.atlassian.net")
	if issue.Description != "First paragraph\nSecond paragraph" {
		t.Errorf("Description = %q", issue.Description)
	}
}

func TestLinearStateToIssueState(t *testing.T) {
	tests := []struct {
		state string
		want  IssueState
	}{
		{"Backlog", IssueStateBacklog},
		{"backlog", IssueStateBacklog},
		{"Todo", IssueStateTodo},
		{"Unstarted", IssueStateTodo},
		{"In Progress", IssueStateInProgress},
		{"Started", IssueStateInProgress},
		{"Done", IssueStateDone},
		{"Completed", IssueStateDone},
		{"Canceled", IssueStateCanceled},
		{"Cancelled", IssueStateCanceled},
		{"Unknown", IssueStateTodo},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			got := linearStateToIssueState(tt.state)
			if got != tt.want {
				t.Errorf("linearStateToIssueState(%q) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

func TestIssueStateToLinearState(t *testing.T) {
	tests := []struct {
		state IssueState
		want  string
	}{
		{IssueStateBacklog, "Backlog"},
		{IssueStateTodo, "Todo"},
		{IssueStateInProgress, "In Progress"},
		{IssueStateDone, "Done"},
		{IssueStateCanceled, "Canceled"},
		{IssueState("unknown"), "Todo"},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			got := issueStateToLinearState(tt.state)
			if got != tt.want {
				t.Errorf("issueStateToLinearState(%q) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

func TestSplitIssueKey(t *testing.T) {
	tests := []struct {
		key        string
		wantPrefix string
		wantNumber string
	}{
		{"ENG-123", "ENG", "123"},
		{"PROJ-1", "PROJ", "1"},
		{"NO-DASH", "NO", "DASH"},
		{"NODASH", "NODASH", ""},
		{"A-B-C", "A-B", "C"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			prefix, number := splitIssueKey(tt.key)
			if prefix != tt.wantPrefix {
				t.Errorf("prefix = %q, want %q", prefix, tt.wantPrefix)
			}
			if number != tt.wantNumber {
				t.Errorf("number = %q, want %q", number, tt.wantNumber)
			}
		})
	}
}

func TestLinearIssueNodeToIssue(t *testing.T) {
	node := &linearIssueNode{
		ID:          "uuid-123",
		Identifier:  "ENG-42",
		Title:       "Test feature",
		Description: "A feature description",
		Priority:    2,
		URL:         "https://linear.app/team/issue/ENG-42",
		CreatedAt:   "2024-01-01T00:00:00Z",
		UpdatedAt:   "2024-01-02T00:00:00Z",
	}
	node.State = &struct {
		Name string `json:"name"`
	}{Name: "In Progress"}
	node.Assignee = &struct {
		Name string `json:"name"`
	}{Name: "Bob"}
	node.Labels.Nodes = []struct {
		Name string `json:"name"`
	}{{Name: "feature"}, {Name: "priority"}}

	issue := node.toIssue()

	if issue.ID != "uuid-123" {
		t.Errorf("ID = %q, want %q", issue.ID, "uuid-123")
	}
	if issue.Key != "ENG-42" {
		t.Errorf("Key = %q, want %q", issue.Key, "ENG-42")
	}
	if issue.Title != "Test feature" {
		t.Errorf("Title = %q, want %q", issue.Title, "Test feature")
	}
	if issue.State != IssueStateInProgress {
		t.Errorf("State = %q, want %q", issue.State, IssueStateInProgress)
	}
	if issue.Priority != 2 {
		t.Errorf("Priority = %d, want %d", issue.Priority, 2)
	}
	if issue.Assignee != "Bob" {
		t.Errorf("Assignee = %q, want %q", issue.Assignee, "Bob")
	}
	if len(issue.Labels) != 2 {
		t.Errorf("Labels count = %d, want 2", len(issue.Labels))
	}
	// Top-level issue with no parent should be a story
	if issue.Type != IssueTypeStory {
		t.Errorf("Type = %q, want %q", issue.Type, IssueTypeStory)
	}
	if issue.ParentKey != "" {
		t.Errorf("ParentKey = %q, want empty", issue.ParentKey)
	}
}

func TestJiraGetIssueWithMockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/ENG-1" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("missing Authorization header")
		}

		resp := jiraIssue{
			ID:  "10001",
			Key: "ENG-1",
		}
		resp.Fields.Summary = "Mock issue"
		resp.Fields.Status.StatusCategory.Key = "new"

		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Set env for the mock server
	os.Setenv("JIRA_URL", server.URL)
	os.Setenv("JIRA_EMAIL", "test@example.com")
	os.Setenv("JIRA_API_TOKEN", "token123")
	defer func() {
		os.Unsetenv("JIRA_URL")
		os.Unsetenv("JIRA_EMAIL")
		os.Unsetenv("JIRA_API_TOKEN")
	}()

	j := NewJira()
	issue, err := j.GetIssue(context.Background(), "ENG-1")
	if err != nil {
		t.Fatalf("GetIssue() error: %v", err)
	}

	if issue.Key != "ENG-1" {
		t.Errorf("Key = %q, want %q", issue.Key, "ENG-1")
	}
	if issue.Title != "Mock issue" {
		t.Errorf("Title = %q, want %q", issue.Title, "Mock issue")
	}
}

func TestLinearGetIssueWithMockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "test-key" {
			t.Error("missing or wrong Authorization header")
		}

		resp := map[string]any{
			"data": map[string]any{
				"issue": map[string]any{
					"id":          "uuid-1",
					"identifier":  "ENG-42",
					"title":       "Linear issue",
					"description": "Test description",
					"priority":    2,
					"url":         "https://linear.app/issue/ENG-42",
					"createdAt":   "2024-01-01T00:00:00Z",
					"updatedAt":   "2024-01-02T00:00:00Z",
					"state":       map[string]any{"name": "In Progress"},
					"assignee":    map[string]any{"name": "Charlie"},
					"labels":      map[string]any{"nodes": []any{}},
					"team":        map[string]any{"key": "ENG"},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	l, err := NewLinear("test-key")
	if err != nil {
		t.Fatalf("NewLinear() error: %v", err)
	}
	// Override the HTTP client to point to mock server
	l.httpClient = server.Client()
	// We need to redirect requests to our mock server - use a custom transport
	l.httpClient = &http.Client{
		Transport: &rewriteTransport{
			url:       server.URL,
			transport: http.DefaultTransport,
		},
	}

	issue, err := l.GetIssue(context.Background(), "ENG-42")
	if err != nil {
		t.Fatalf("GetIssue() error: %v", err)
	}

	if issue.Key != "ENG-42" {
		t.Errorf("Key = %q, want %q", issue.Key, "ENG-42")
	}
	if issue.Title != "Linear issue" {
		t.Errorf("Title = %q, want %q", issue.Title, "Linear issue")
	}
	if issue.State != IssueStateInProgress {
		t.Errorf("State = %q, want %q", issue.State, IssueStateInProgress)
	}
}

func TestJiraIssueTypeToIssueType(t *testing.T) {
	tests := []struct {
		name string
		want IssueType
	}{
		{"Epic", IssueTypeEpic},
		{"epic", IssueTypeEpic},
		{"Story", IssueTypeStory},
		{"story", IssueTypeStory},
		{"Task", IssueTypeTask},
		{"Sub-task", IssueTypeTask},
		{"subtask", IssueTypeTask},
		{"Bug", IssueTypeBug},
		{"bug", IssueTypeBug},
		{"Something Else", IssueTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jiraIssueTypeToIssueType(tt.name)
			if got != tt.want {
				t.Errorf("jiraIssueTypeToIssueType(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestIssueTypeToJiraName(t *testing.T) {
	tests := []struct {
		issueType IssueType
		want      string
	}{
		{IssueTypeEpic, "Epic"},
		{IssueTypeStory, "Story"},
		{IssueTypeTask, "Task"},
		{IssueTypeBug, "Bug"},
		{IssueTypeUnknown, "Task"},
	}

	for _, tt := range tests {
		t.Run(string(tt.issueType), func(t *testing.T) {
			got := issueTypeToJiraName(tt.issueType)
			if got != tt.want {
				t.Errorf("issueTypeToJiraName(%q) = %q, want %q", tt.issueType, got, tt.want)
			}
		})
	}
}

func TestJiraIssueToIssueWithType(t *testing.T) {
	t.Run("epic with no parent", func(t *testing.T) {
		ji := &jiraIssue{ID: "1", Key: "PROJ-100"}
		ji.Fields.Summary = "Epic issue"
		ji.Fields.Status.StatusCategory.Key = "new"
		ji.Fields.IssueType = &struct {
			Name string `json:"name"`
		}{Name: "Epic"}

		issue := jiraIssueToIssue(ji, "https://test.atlassian.net")
		if issue.Type != IssueTypeEpic {
			t.Errorf("Type = %q, want %q", issue.Type, IssueTypeEpic)
		}
		if issue.ParentKey != "" {
			t.Errorf("ParentKey = %q, want empty", issue.ParentKey)
		}
	})

	t.Run("story with parent epic", func(t *testing.T) {
		ji := &jiraIssue{ID: "2", Key: "ENG-456"}
		ji.Fields.Summary = "Story issue"
		ji.Fields.Status.StatusCategory.Key = "indeterminate"
		ji.Fields.IssueType = &struct {
			Name string `json:"name"`
		}{Name: "Story"}
		ji.Fields.Parent = &struct {
			Key string `json:"key"`
		}{Key: "PROJ-100"}

		issue := jiraIssueToIssue(ji, "https://test.atlassian.net")
		if issue.Type != IssueTypeStory {
			t.Errorf("Type = %q, want %q", issue.Type, IssueTypeStory)
		}
		if issue.ParentKey != "PROJ-100" {
			t.Errorf("ParentKey = %q, want %q", issue.ParentKey, "PROJ-100")
		}
	})

	t.Run("no issuetype defaults to unknown", func(t *testing.T) {
		ji := &jiraIssue{ID: "3", Key: "ENG-789"}
		ji.Fields.Summary = "Unknown type"
		ji.Fields.Status.StatusCategory.Key = "new"

		issue := jiraIssueToIssue(ji, "https://test.atlassian.net")
		if issue.Type != IssueTypeUnknown {
			t.Errorf("Type = %q, want %q", issue.Type, IssueTypeUnknown)
		}
	})
}

func TestLinearIssueNodeToIssueWithHierarchy(t *testing.T) {
	t.Run("sub-issue has parent and task type", func(t *testing.T) {
		node := &linearIssueNode{
			ID:         "uuid-child",
			Identifier: "ENG-43",
			Title:      "Sub-issue",
		}
		node.Parent = &struct {
			ID         string `json:"id"`
			Identifier string `json:"identifier"`
		}{ID: "uuid-parent", Identifier: "ENG-42"}

		issue := node.toIssue()
		if issue.Type != IssueTypeTask {
			t.Errorf("Type = %q, want %q", issue.Type, IssueTypeTask)
		}
		if issue.ParentKey != "ENG-42" {
			t.Errorf("ParentKey = %q, want %q", issue.ParentKey, "ENG-42")
		}
	})

	t.Run("issue with children", func(t *testing.T) {
		node := &linearIssueNode{
			ID:         "uuid-parent",
			Identifier: "ENG-42",
			Title:      "Parent issue",
		}
		node.Children.Nodes = []struct {
			Identifier string `json:"identifier"`
		}{
			{Identifier: "ENG-43"},
			{Identifier: "ENG-44"},
		}

		issue := node.toIssue()
		if issue.Type != IssueTypeStory {
			t.Errorf("Type = %q, want %q", issue.Type, IssueTypeStory)
		}
		if len(issue.Children) != 2 {
			t.Errorf("Children count = %d, want 2", len(issue.Children))
		}
		if issue.Children[0] != "ENG-43" {
			t.Errorf("Children[0] = %q, want %q", issue.Children[0], "ENG-43")
		}
	})
}

func TestLinearProjectNodeToIssue(t *testing.T) {
	node := &linearProjectNode{
		ID:          "proj-uuid",
		Name:        "Q1 Auth Project",
		Description: "Authentication overhaul",
		URL:         "https://linear.app/team/project/proj-uuid",
		State:       "started",
		CreatedAt:   "2024-01-01T00:00:00Z",
		UpdatedAt:   "2024-06-01T00:00:00Z",
	}
	node.Issues.Nodes = []struct {
		Identifier string `json:"identifier"`
	}{
		{Identifier: "ENG-10"},
		{Identifier: "ENG-11"},
		{Identifier: "ENG-12"},
	}

	issue := node.toIssue()

	if issue.Type != IssueTypeEpic {
		t.Errorf("Type = %q, want %q", issue.Type, IssueTypeEpic)
	}
	if issue.Title != "Q1 Auth Project" {
		t.Errorf("Title = %q, want %q", issue.Title, "Q1 Auth Project")
	}
	if issue.Description != "Authentication overhaul" {
		t.Errorf("Description = %q, want %q", issue.Description, "Authentication overhaul")
	}
	if len(issue.Children) != 3 {
		t.Errorf("Children count = %d, want 3", len(issue.Children))
	}
	if issue.Children[0] != "ENG-10" {
		t.Errorf("Children[0] = %q, want %q", issue.Children[0], "ENG-10")
	}
}

func TestJiraGetIssueWithTypeFromMockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := jiraIssue{
			ID:  "10001",
			Key: "PROJ-100",
		}
		resp.Fields.Summary = "Epic issue"
		resp.Fields.Status.StatusCategory.Key = "new"
		resp.Fields.IssueType = &struct {
			Name string `json:"name"`
		}{Name: "Epic"}

		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	os.Setenv("JIRA_URL", server.URL)
	os.Setenv("JIRA_EMAIL", "test@example.com")
	os.Setenv("JIRA_API_TOKEN", "token123")
	defer func() {
		os.Unsetenv("JIRA_URL")
		os.Unsetenv("JIRA_EMAIL")
		os.Unsetenv("JIRA_API_TOKEN")
	}()

	j := NewJira()
	issue, err := j.GetIssue(context.Background(), "PROJ-100")
	if err != nil {
		t.Fatalf("GetIssue() error: %v", err)
	}

	if issue.Type != IssueTypeEpic {
		t.Errorf("Type = %q, want %q", issue.Type, IssueTypeEpic)
	}
}

// rewriteTransport redirects all requests to a test server URL.
type rewriteTransport struct {
	url       string
	transport http.RoundTripper
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = t.url[len("http://"):]
	return t.transport.RoundTrip(req)
}
