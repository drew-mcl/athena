package vcs

import (
	"testing"
)

func TestNewGitHub(t *testing.T) {
	g := NewGitHub()
	if g.Name() != "github" {
		t.Errorf("Name() = %q, want %q", g.Name(), "github")
	}
	if g.Category() != "vcs" {
		t.Errorf("Category() = %q, want %q", g.Category(), "vcs")
	}
	if g.Enabled() {
		t.Error("should be disabled by default")
	}
}

func TestNewGitLab(t *testing.T) {
	g := NewGitLab()
	if g.Name() != "gitlab" {
		t.Errorf("Name() = %q, want %q", g.Name(), "gitlab")
	}
	if g.Category() != "vcs" {
		t.Errorf("Category() = %q, want %q", g.Category(), "vcs")
	}
}

func TestGhStateToPRState(t *testing.T) {
	tests := []struct {
		input string
		want  PRState
	}{
		{"OPEN", PRStateOpen},
		{"open", PRStateOpen},
		{"MERGED", PRStateMerged},
		{"merged", PRStateMerged},
		{"CLOSED", PRStateClosed},
		{"closed", PRStateClosed},
		{"unknown", PRStateOpen},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ghStateToPRState(tt.input)
			if got != tt.want {
				t.Errorf("ghStateToPRState(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGlabStateToPRState(t *testing.T) {
	tests := []struct {
		input string
		want  PRState
	}{
		{"opened", PRStateOpen},
		{"Opened", PRStateOpen},
		{"merged", PRStateMerged},
		{"closed", PRStateClosed},
		{"unknown", PRStateOpen},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := glabStateToPRState(tt.input)
			if got != tt.want {
				t.Errorf("glabStateToPRState(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGhCIStatusToCIStatus(t *testing.T) {
	tests := []struct {
		status     string
		conclusion string
		want       CIStatus
	}{
		{"queued", "", CIStatusPending},
		{"pending", "", CIStatusPending},
		{"in_progress", "", CIStatusRunning},
		{"completed", "success", CIStatusPassed},
		{"completed", "failure", CIStatusFailed},
		{"completed", "cancelled", CIStatusFailed},
		{"unknown", "", CIStatusPending},
	}

	for _, tt := range tests {
		name := tt.status
		if tt.conclusion != "" {
			name += "/" + tt.conclusion
		}
		t.Run(name, func(t *testing.T) {
			got := ghCIStatusToCIStatus(tt.status, tt.conclusion)
			if got != tt.want {
				t.Errorf("ghCIStatusToCIStatus(%q, %q) = %q, want %q", tt.status, tt.conclusion, got, tt.want)
			}
		})
	}
}

func TestGlabCIStatusToCIStatus(t *testing.T) {
	tests := []struct {
		status string
		want   CIStatus
	}{
		{"pending", CIStatusPending},
		{"created", CIStatusPending},
		{"running", CIStatusRunning},
		{"success", CIStatusPassed},
		{"failed", CIStatusFailed},
		{"canceled", CIStatusFailed},
		{"skipped", CIStatusFailed},
		{"unknown", CIStatusPending},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := glabCIStatusToCIStatus(tt.status)
			if got != tt.want {
				t.Errorf("glabCIStatusToCIStatus(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestPRStateConstants(t *testing.T) {
	if PRStateOpen != "open" {
		t.Errorf("PRStateOpen = %q, want open", PRStateOpen)
	}
	if PRStateMerged != "merged" {
		t.Errorf("PRStateMerged = %q, want merged", PRStateMerged)
	}
	if PRStateClosed != "closed" {
		t.Errorf("PRStateClosed = %q, want closed", PRStateClosed)
	}
}

func TestCIStatusConstants(t *testing.T) {
	if CIStatusPending != "pending" {
		t.Errorf("CIStatusPending = %q, want pending", CIStatusPending)
	}
	if CIStatusRunning != "running" {
		t.Errorf("CIStatusRunning = %q, want running", CIStatusRunning)
	}
	if CIStatusPassed != "passed" {
		t.Errorf("CIStatusPassed = %q, want passed", CIStatusPassed)
	}
	if CIStatusFailed != "failed" {
		t.Errorf("CIStatusFailed = %q, want failed", CIStatusFailed)
	}
}
