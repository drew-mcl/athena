package store

import (
	"context"
	"strings"
	"testing"
)

func TestGetLatestSnapshotReturnsErrorForInvalidMetadataJSON(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	if err := st.CreateAgent(&Agent{
		ID:           "agent-snapshot-test",
		WorktreePath: "/tmp/wt",
		ProjectName:  "athena",
		Archetype:    "executor",
		Status:       AgentStatusRunning,
	}); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}

	_, err := st.db.Exec(`
		INSERT INTO snapshots (
			id, agent_id, sequence, timestamp, checksum, data,
			message_count, tool_call_count, duration_ms, is_complete, metadata
		) VALUES (?, ?, ?, CURRENT_TIMESTAMP, ?, ?, ?, ?, ?, ?, ?)
	`, "snapshot-bad-metadata", "agent-snapshot-test", 1, "checksum", "{}", 1, 0, 0, false, "{bad-json")
	if err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}

	_, err = st.GetLatestSnapshot(context.Background(), "agent-snapshot-test")
	if err == nil {
		t.Fatal("expected error for invalid metadata JSON")
	}
	if !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("error should mention metadata, got: %v", err)
	}
}
