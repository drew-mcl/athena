package daemon

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/logging"
	"github.com/drewfead/athena/internal/store"
)

// rateLimitState tracks daemon-wide rate limit status.
// Rate limits are ephemeral (no DB persistence needed).
type rateLimitState struct {
	mu       sync.RWMutex
	limited  bool
	resetAt  time.Time
	reason   string
	agentID  string
	hitCount int
	notify   chan struct{} // closed when rate limit clears
}

func newRateLimitState() *rateLimitState {
	return &rateLimitState{
		notify: make(chan struct{}),
	}
}

// setRateLimited marks the daemon as rate limited until resetAt.
func (r *rateLimitState) setRateLimited(resetAt time.Time, reason, agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// If already limited with a later reset, keep the later one
	if r.limited && resetAt.Before(r.resetAt) {
		r.hitCount++
		return
	}

	wasLimited := r.limited
	r.limited = true
	r.resetAt = resetAt
	r.reason = reason
	r.agentID = agentID
	r.hitCount++

	// Create a new notify channel if needed
	if !wasLimited {
		r.notify = make(chan struct{})
	}
}

// clearRateLimit marks the rate limit as cleared.
func (r *rateLimitState) clearRateLimit() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.limited {
		return
	}

	r.limited = false
	close(r.notify) // Wake up all waiters
}

// isRateLimited returns whether the daemon is currently rate limited and when it resets.
func (r *rateLimitState) isRateLimited() (bool, time.Time) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.limited {
		return false, time.Time{}
	}

	// Auto-clear if reset time has passed
	if time.Now().After(r.resetAt) {
		r.mu.RUnlock()
		r.clearRateLimit()
		r.mu.RLock()
		return false, time.Time{}
	}

	return r.limited, r.resetAt
}

// waitForRateLimit blocks until the rate limit clears or the context is cancelled.
func (r *rateLimitState) waitForRateLimit(ctx context.Context) error {
	r.mu.RLock()
	if !r.limited {
		r.mu.RUnlock()
		return nil
	}
	resetAt := r.resetAt
	notifyCh := r.notify
	r.mu.RUnlock()

	// Wait until reset time or context cancellation
	timer := time.NewTimer(time.Until(resetAt))
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-notifyCh:
		return nil
	case <-timer.C:
		r.clearRateLimit()
		return nil
	}
}

// buildStatus creates a RateLimitStatus for API responses.
func (r *rateLimitState) buildStatus() *control.RateLimitStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	status := &control.RateLimitStatus{
		HitCount: r.hitCount,
	}

	if !r.limited || time.Now().After(r.resetAt) {
		return status
	}

	status.Limited = true
	status.ResetAt = r.resetAt.Format(time.RFC3339)
	status.Reason = r.reason
	status.AgentID = r.agentID
	status.WaitingSec = int(time.Until(r.resetAt).Seconds())
	if status.WaitingSec < 0 {
		status.WaitingSec = 0
	}

	return status
}

// Daemon integration

func (d *Daemon) handleRateLimitStatus(_ json.RawMessage) (any, error) {
	return d.rateLimit.buildStatus(), nil
}

// onRateLimitDetected is called when an agent hits a rate limit.
// It updates the daemon-wide state and marks the agent as rate_limited.
func (d *Daemon) onRateLimitDetected(resetAt time.Time, reason, agentID string) {
	logging.Warn("rate limit detected",
		"agent_id", agentID,
		"reset_at", resetAt,
		"reason", truncateForLog(reason, 100),
	)

	d.rateLimit.setRateLimited(resetAt, reason, agentID)

	// Broadcast rate limit event
	d.server.Broadcast(control.Event{
		Type:    "rate_limited",
		Payload: d.rateLimit.buildStatus(),
	})
}

// rateLimitRecovery runs after a rate limit clears, resuming rate-limited agents.
func (d *Daemon) rateLimitRecovery() {
	agents, err := d.store.ListAgents(store.AgentStatusRateLimited)
	if err != nil {
		logging.Warn("failed to list rate-limited agents for recovery", "error", err)
		return
	}

	if len(agents) == 0 {
		return
	}

	logging.Info("rate limit cleared, recovering agents", "count", len(agents))

	// Broadcast rate limit cleared event
	d.server.Broadcast(control.Event{
		Type:    "rate_limit_cleared",
		Payload: d.rateLimit.buildStatus(),
	})

	for _, agent := range agents {
		logging.Info("resuming rate-limited agent", "agent_id", agent.ID)

		// Reset to pending so the supervisor picks it up for restart
		d.store.UpdateAgentStatus(agent.ID, store.AgentStatusCrashed)
	}
}
