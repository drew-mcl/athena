package control

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRequestSerialization(t *testing.T) {
	req := Request{
		Method: "list_agents",
		Params: json.RawMessage(`{"status":"running"}`),
		ID:     "req-1",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Request
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Method != "list_agents" {
		t.Errorf("Method = %q, want list_agents", decoded.Method)
	}
	if decoded.ID != "req-1" {
		t.Errorf("ID = %q, want req-1", decoded.ID)
	}
}

func TestResponseSerialization(t *testing.T) {
	t.Run("success response", func(t *testing.T) {
		resp := Response{
			Data: map[string]string{"status": "ok"},
			ID:   "resp-1",
		}

		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var decoded Response
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if decoded.ID != "resp-1" {
			t.Errorf("ID = %q, want resp-1", decoded.ID)
		}
		if decoded.Error != "" {
			t.Errorf("Error should be empty, got %q", decoded.Error)
		}
	})

	t.Run("error response", func(t *testing.T) {
		resp := Response{
			Error: "not found",
			ID:    "resp-2",
		}

		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var decoded Response
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if decoded.Error != "not found" {
			t.Errorf("Error = %q, want not found", decoded.Error)
		}
	})
}

func TestEventSerialization(t *testing.T) {
	event := Event{
		Type:    "agent_created",
		Payload: map[string]string{"id": "agent-1"},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Type != "agent_created" {
		t.Errorf("Type = %q, want agent_created", decoded.Type)
	}
}

func TestSpawnAgentRequestSerialization(t *testing.T) {
	req := SpawnAgentRequest{
		WorktreePath: "/path/to/worktree",
		Archetype:    "planner",
		Prompt:       "Plan this feature",
		Provider:     "claude",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SpawnAgentRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.WorktreePath != "/path/to/worktree" {
		t.Errorf("WorktreePath = %q", decoded.WorktreePath)
	}
	if decoded.Archetype != "planner" {
		t.Errorf("Archetype = %q", decoded.Archetype)
	}
}

func TestAgentInfoSerialization(t *testing.T) {
	info := AgentInfo{
		ID:           "agent-1",
		WorktreePath: "/path/to/worktree",
		ProjectName:  "athena",
		Archetype:    "executor",
		Status:       "running",
		RestartCount: 0,
		CreatedAt:    "2024-01-01T00:00:00Z",
		PlanStatus:   "draft",
		Metrics: &AgentMetrics{
			ToolUseCount: 10,
			FilesRead:    5,
			FilesWritten: 2,
			CostCents:    42,
		},
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded AgentInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ID != "agent-1" {
		t.Errorf("ID = %q", decoded.ID)
	}
	if decoded.PlanStatus != "draft" {
		t.Errorf("PlanStatus = %q", decoded.PlanStatus)
	}
	if decoded.Metrics == nil {
		t.Fatal("Metrics is nil")
	}
	if decoded.Metrics.ToolUseCount != 10 {
		t.Errorf("ToolUseCount = %d", decoded.Metrics.ToolUseCount)
	}
	if decoded.Metrics.CostCents != 42 {
		t.Errorf("CostCents = %d", decoded.Metrics.CostCents)
	}
}

func TestWorktreeInfoSerialization(t *testing.T) {
	info := WorktreeInfo{
		Path:        "/repos/worktrees/ENG-123-a1b2",
		Project:     "athena",
		Branch:      "feat/eng-123",
		IsMain:      false,
		TicketID:    "ENG-123",
		TicketHash:  "a1b2",
		Description: "Add login feature",
		WTStatus:    "active",
		Ahead:       3,
		Behind:      1,
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded WorktreeInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.TicketID != "ENG-123" {
		t.Errorf("TicketID = %q", decoded.TicketID)
	}
	if decoded.Ahead != 3 {
		t.Errorf("Ahead = %d", decoded.Ahead)
	}
	if decoded.Behind != 1 {
		t.Errorf("Behind = %d", decoded.Behind)
	}
}

func TestCreateWorktreeRequestSerialization(t *testing.T) {
	req := CreateWorktreeRequest{
		MainRepoPath: "/repos/athena",
		Branch:       "feat/eng-123",
		TicketID:     "ENG-123",
		Description:  "Add login feature",
		WorkflowMode: "approve",
		UseQueueHead: true,
		StartPoint:   "feat/eng-122",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded CreateWorktreeRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.UseQueueHead != true {
		t.Errorf("UseQueueHead = %v", decoded.UseQueueHead)
	}
	if decoded.StartPoint != "feat/eng-122" {
		t.Errorf("StartPoint = %q", decoded.StartPoint)
	}
}

func TestJobInfoSerialization(t *testing.T) {
	job := JobInfo{
		ID:              "job-1",
		RawInput:        "add login feature",
		NormalizedInput: "Add login feature to the application",
		Status:          "executing",
		Type:            "feature",
		Project:         "athena",
	}

	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded JobInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Type != "feature" {
		t.Errorf("Type = %q", decoded.Type)
	}
}

func TestStreamEventBuilder(t *testing.T) {
	event := NewStreamEvent(StreamEventToolCall, StreamSourceAgent).
		WithAgent("agent-1").
		WithWorktree("/path/to/wt").
		WithPayload(ToolCallPayload{ToolName: "Bash", ToolUseID: "tc-1"})

	if event.Type != StreamEventToolCall {
		t.Errorf("Type = %q, want tool_call", event.Type)
	}
	if event.Source != StreamSourceAgent {
		t.Errorf("Source = %q, want agent", event.Source)
	}
	if event.AgentID != "agent-1" {
		t.Errorf("AgentID = %q", event.AgentID)
	}
	if event.WorktreePath != "/path/to/wt" {
		t.Errorf("WorktreePath = %q", event.WorktreePath)
	}
	if event.ID == "" {
		t.Error("ID should not be empty")
	}
	if event.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}

	// Verify payload
	var payload ToolCallPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.ToolName != "Bash" {
		t.Errorf("payload ToolName = %q", payload.ToolName)
	}
}

func TestStreamEventTypes(t *testing.T) {
	types := []StreamEventType{
		StreamEventAgentCreated,
		StreamEventAgentStarted,
		StreamEventAgentTerminated,
		StreamEventAgentCrashed,
		StreamEventThinking,
		StreamEventToolCall,
		StreamEventToolResult,
		StreamEventMessage,
		StreamEventHeartbeat,
		StreamEventJobCreated,
		StreamEventWorktreeCreated,
		StreamEventPlanCreated,
		StreamEventDaemonStarted,
	}

	for _, st := range types {
		if string(st) == "" {
			t.Errorf("empty stream event type")
		}
	}
}

func TestSubscribeStreamRequestSerialization(t *testing.T) {
	req := SubscribeStreamRequest{
		AgentID:        "agent-1",
		WorktreePath:   "/path",
		EventTypes:     []StreamEventType{StreamEventToolCall, StreamEventMessage},
		HistorySeconds: 60,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SubscribeStreamRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.AgentID != "agent-1" {
		t.Errorf("AgentID = %q", decoded.AgentID)
	}
	if len(decoded.EventTypes) != 2 {
		t.Errorf("EventTypes len = %d, want 2", len(decoded.EventTypes))
	}
	if decoded.HistorySeconds != 60 {
		t.Errorf("HistorySeconds = %d", decoded.HistorySeconds)
	}
}

func TestMergeQueueItemInfoSerialization(t *testing.T) {
	item := MergeQueueItemInfo{
		ID:           "mq-1",
		Project:      "athena",
		WorktreePath: "/path/to/wt",
		Branch:       "feat/login",
		Position:     1,
		Status:       "queued",
		BaseBranch:   "main",
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded MergeQueueItemInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Position != 1 {
		t.Errorf("Position = %d", decoded.Position)
	}
	if decoded.Status != "queued" {
		t.Errorf("Status = %q", decoded.Status)
	}
}

func TestPruneWorktreesResultSerialization(t *testing.T) {
	result := PruneWorktreesResult{
		Merged:  []string{"/path/1"},
		Orphans: []string{"/path/2"},
		Stale:   []string{"/path/3"},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded PruneWorktreesResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.Merged) != 1 || decoded.Merged[0] != "/path/1" {
		t.Errorf("Merged = %v", decoded.Merged)
	}
}

func TestNewStreamEventTimestamp(t *testing.T) {
	before := time.Now()
	event := NewStreamEvent(StreamEventMessage, StreamSourceDaemon)
	after := time.Now()

	if event.Timestamp.Before(before) || event.Timestamp.After(after) {
		t.Error("timestamp should be between before and after")
	}
}

func TestStreamEventWithNilPayload(t *testing.T) {
	event := NewStreamEvent(StreamEventHeartbeat, StreamSourceDaemon).
		WithPayload(nil)

	if event.Payload != nil {
		t.Errorf("payload should be nil, got %s", string(event.Payload))
	}
}

func TestStreamEventWithUnsupportedPayload(t *testing.T) {
	event := NewStreamEvent(StreamEventHeartbeat, StreamSourceDaemon).
		WithPayload(func() {})

	if len(event.Payload) == 0 {
		t.Fatal("payload should contain fallback error data")
	}

	var decoded map[string]string
	if err := json.Unmarshal(event.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal fallback payload: %v", err)
	}
	if decoded["error"] != "payload_encoding_failed" {
		t.Errorf("fallback error = %q", decoded["error"])
	}
}

func TestWorkItemInfoSerialization(t *testing.T) {
	item := WorkItemInfo{
		ID:          "wi-a3f8",
		Project:     "athena",
		ItemType:    "feature",
		ParentID:    "wi-b1c2",
		Subject:     "Add login",
		Description: "Implement OAuth login",
		Status:      "in_progress",
		Priority:    2,
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded WorkItemInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ItemType != "feature" {
		t.Errorf("ItemType = %q", decoded.ItemType)
	}
	if decoded.ParentID != "wi-b1c2" {
		t.Errorf("ParentID = %q", decoded.ParentID)
	}
}
