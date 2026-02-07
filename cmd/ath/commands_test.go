package main

import "testing"

// runIDMatchTests is a generic helper for table-driven bool-function tests.
func runIDMatchTests(t *testing.T, fn func(string) bool, tests []struct {
	input string
	want  bool
}) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := fn(tt.input)
			if got != tt.want {
				t.Errorf("%q = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsTicketID(t *testing.T) {
	runIDMatchTests(t, isTicketID, []struct {
		input string
		want  bool
	}{
		// Valid ticket IDs
		{"ENG-123", true},
		{"PROJ-45", true},
		{"AB-1", true},
		{"eng-123", true},
		{"Data-99", true},
		{"LONGPREFIX-1", true},

		// Invalid ticket IDs
		{"", false},
		{"123", false},
		{"wi-abc", false},
		{"just-text", false},
		{"E-123", false},
		{"TOOLONGPREFIX-1", false},
		{"ENG", false},
		{"ENG-", false},
		{"-123", false},
		{"123-456", false},
		{"ENG-abc", false},
	})
}

func TestIsWorkItemID(t *testing.T) {
	runIDMatchTests(t, isWorkItemID, []struct {
		input string
		want  bool
	}{
		// Valid work item IDs
		{"wi-a3f8", true},
		{"wi-a3f8.1", true},
		{"wi-abcd.2.3", true},
		{"wi-", true},

		// Invalid work item IDs
		{"", false},
		{"WI-a3f8", false},
		{"a3f8", false},
		{"feat-123", false},
		{"wia3f8", false},
	})
}
