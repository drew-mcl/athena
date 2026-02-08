package store

import (
	"strings"
	"testing"
)

func TestGetJobHandlesNullableJSONColumns(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	_, err := st.db.Exec(`
		INSERT INTO jobs (
			id, raw_input, normalized_input, status, job_type, project, agent_history, propagation_results
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "job-null-json", "raw", "normalized", JobStatusPending, JobTypeFeature, "athena", nil, nil)
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}

	job, err := st.GetJob("job-null-json")
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	if job == nil {
		t.Fatal("expected job")
	}
	if len(job.AgentHistory) != 0 {
		t.Errorf("AgentHistory len = %d, want 0", len(job.AgentHistory))
	}
	if len(job.PropagationResults) != 0 {
		t.Errorf("PropagationResults len = %d, want 0", len(job.PropagationResults))
	}
}

func TestGetJobReturnsErrorForInvalidAgentHistoryJSON(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	_, err := st.db.Exec(`
		INSERT INTO jobs (
			id, raw_input, normalized_input, status, job_type, project, agent_history, propagation_results
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "job-bad-history", "raw", "normalized", JobStatusPending, JobTypeFeature, "athena", "{bad-json", "[]")
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}

	_, err = st.GetJob("job-bad-history")
	if err == nil {
		t.Fatal("expected error for invalid agent_history JSON")
	}
	if !strings.Contains(err.Error(), "agent_history") {
		t.Fatalf("error should mention agent_history, got: %v", err)
	}
}

func TestListJobsReturnsErrorForInvalidPropagationJSON(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	_, err := st.db.Exec(`
		INSERT INTO jobs (
			id, raw_input, normalized_input, status, job_type, project, agent_history, propagation_results
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "job-bad-propagation", "raw", "normalized", JobStatusPending, JobTypeFeature, "athena", "[]", "{bad-json")
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}

	_, err = st.ListJobs()
	if err == nil {
		t.Fatal("expected error for invalid propagation_results JSON")
	}
	if !strings.Contains(err.Error(), "propagation_results") {
		t.Fatalf("error should mention propagation_results, got: %v", err)
	}
}
