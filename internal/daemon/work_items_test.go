package daemon

import (
	"testing"
)

func TestTaskListDirToWorkItemID(t *testing.T) {
	tests := []struct {
		name     string
		dirName  string
		expected string
	}{
		{
			name:     "goal with hex hash unchanged",
			dirName:  "wi-a3f8",
			expected: "wi-a3f8",
		},
		{
			name:     "goal with all-digit hash unchanged",
			dirName:  "wi-1234",
			expected: "wi-1234",
		},
		{
			name:     "feature child converts last hyphen to dot",
			dirName:  "wi-a3f8-2",
			expected: "wi-a3f8.2",
		},
		{
			name:     "feature under all-digit goal",
			dirName:  "wi-1234-3",
			expected: "wi-1234.3",
		},
		{
			name:     "non-wi prefix unchanged",
			dirName:  "random-name",
			expected: "random-name",
		},
		{
			name:     "non-digit suffix unchanged",
			dirName:  "wi-abcd-feat",
			expected: "wi-abcd-feat",
		},
		{
			name:     "bare wi prefix no hash",
			dirName:  "wi",
			expected: "wi",
		},
		{
			name:     "empty string",
			dirName:  "",
			expected: "",
		},
		{
			name:     "trailing hyphen unchanged",
			dirName:  "wi-abcd-",
			expected: "wi-abcd-",
		},
		{
			name:     "deeply nested converts only last digit segment",
			dirName:  "wi-a3f8-2-1",
			expected: "wi-a3f8-2.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := taskListDirToWorkItemID(tt.dirName)
			if got != tt.expected {
				t.Errorf("taskListDirToWorkItemID(%q) = %q, want %q", tt.dirName, got, tt.expected)
			}
		})
	}
}

func TestExtractWorkItemIDFromWorktreePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "feature worktree",
			path:     "/Users/drew/repos/worktrees/wi-c5a6.1-1925",
			expected: "wi-c5a6.1",
		},
		{
			name:     "goal worktree",
			path:     "/Users/drew/repos/worktrees/wi-266d.1-d193",
			expected: "wi-266d.1",
		},
		{
			name:     "deep feature worktree",
			path:     "/Users/drew/repos/worktrees/wi-c5a6.10-0b7e",
			expected: "wi-c5a6.10",
		},
		{
			name:     "non-wi path",
			path:     "/Users/drew/repos/athena",
			expected: "",
		},
		{
			name:     "empty path",
			path:     "",
			expected: "",
		},
		{
			name:     "hash too long",
			path:     "/Users/drew/repos/worktrees/wi-c5a6.1-12345",
			expected: "",
		},
		{
			name:     "hash too short",
			path:     "/Users/drew/repos/worktrees/wi-c5a6.1-abc",
			expected: "",
		},
		{
			name:     "non-hex hash",
			path:     "/Users/drew/repos/worktrees/wi-c5a6.1-gggg",
			expected: "",
		},
		{
			name:     "no hash suffix just wi-id",
			path:     "/Users/drew/repos/worktrees/wi-c5a6",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractWorkItemIDFromWorktreePath(tt.path)
			if got != tt.expected {
				t.Errorf("extractWorkItemIDFromWorktreePath(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}
