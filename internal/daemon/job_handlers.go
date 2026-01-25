package daemon

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/logging"
	"github.com/drewfead/athena/internal/store"
)

func (d *Daemon) handleListJobs(_ json.RawMessage) (any, error) {
	jobs, err := d.store.ListJobs()
	if err != nil {
		return nil, err
	}

	var result []*control.JobInfo
	for _, j := range jobs {
		result = append(result, jobToInfo(j))
	}
	return result, nil
}

func jobToInfo(j *store.Job) *control.JobInfo {
	info := &control.JobInfo{
		ID:              j.ID,
		RawInput:        j.RawInput,
		NormalizedInput: j.NormalizedInput,
		Status:          string(j.Status),
		Type:            string(j.Type),
		Project:         j.Project,
		CreatedAt:       j.CreatedAt.Format(time.RFC3339),
	}
	if j.CurrentAgentID != nil {
		info.AgentID = *j.CurrentAgentID
	}
	if j.ExternalID != nil {
		info.ExternalID = *j.ExternalID
	}
	if j.ExternalURL != nil {
		info.ExternalURL = *j.ExternalURL
	}
	if j.Answer != nil {
		info.Answer = *j.Answer
	}
	if j.WorktreePath != nil {
		info.WorktreePath = *j.WorktreePath
	}
	return info
}

// normalizeInput performs basic input cleaning and normalization.
func normalizeInput(input string) string {
	// Trim whitespace
	input = strings.TrimSpace(input)

	// Remove excessive whitespace
	re := regexp.MustCompile(`\s+`)
	input = re.ReplaceAllString(input, " ")

	// Limit length to prevent abuse
	if len(input) > 10000 {
		input = input[:10000] + "..."
	}

	return input
}

func (d *Daemon) handleCreateJob(params json.RawMessage) (any, error) {
	// Reject new jobs if we're draining
	if d.isDraining() {
		return nil, fmt.Errorf("daemon is shutting down, not accepting new jobs")
	}

	var req control.CreateJobRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	// Determine job type (default to feature)
	jobType := store.JobType(req.Type)
	if jobType == "" {
		jobType = store.JobTypeFeature
	}

	// Normalize input with basic cleaning
	job := &store.Job{
		ID:              generateID(),
		RawInput:        req.Input,
		NormalizedInput: normalizeInput(req.Input),
		Status:          store.JobStatusPending,
		Type:            jobType,
		Project:         req.Project,
	}

	// Set target branch for quick jobs
	if jobType == store.JobTypeQuick && req.TargetBranch != "" {
		job.TargetBranch = &req.TargetBranch
	}

	if err := d.store.CreateJob(job); err != nil {
		return nil, err
	}

	// Queue for execution
	select {
	case d.jobQueue <- job.ID:
		logging.Debug("queued job for execution", "job_id", job.ID)
	default:
		logging.Warn("job queue full, will retry on restart", "job_id", job.ID)
	}

	// Broadcast event
	d.server.Broadcast(control.Event{
		Type:    "job_created",
		Payload: jobToInfo(job),
	})

	// Emit stream event for visualization
	d.EmitStreamEvent(control.NewStreamEvent(control.StreamEventJobCreated, control.StreamSourceDaemon).
		WithPayload(map[string]any{
			"job_id":  job.ID,
			"type":    string(job.Type),
			"project": job.Project,
		}))

	return jobToInfo(job), nil
}
