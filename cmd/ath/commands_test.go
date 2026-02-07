package main

import "testing"

func TestIsTicketID(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Valid ticket IDs
		{"ENG-123", true},
		{"PROJ-45", true},
		{"AB-1", true},
		{"eng-123", true},   // lowercase letters OK
		{"Data-99", true},   // mixed case OK
		{"LONGPREFIX-1", true}, // 10-char prefix (max)

		// Invalid ticket IDs
		{"", false},
		{"123", false},           // no prefix
		{"wi-abc", false},        // prefix too short (2-char minimum, "wi" is 2 but "abc" is not a number)
		{"just-text", false},     // number part doesn't start with digit
		{"E-123", false},         // prefix too short (1 char)
		{"TOOLONGPREFIX-1", false}, // prefix > 10 chars (13 chars)
		{"ENG", false},           // no hyphen
		{"ENG-", false},          // empty number part
		{"-123", false},          // empty prefix
		{"123-456", false},       // numeric prefix
		{"ENG-abc", false},       // non-numeric suffix
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isTicketID(tt.input)
			if got != tt.want {
				t.Errorf("isTicketID(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsWorkItemID(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Valid work item IDs
		{"wi-a3f8", true},
		{"wi-a3f8.1", true},
		{"wi-abcd.2.3", true},
		{"wi-", true}, // prefix match only

		// Invalid work item IDs
		{"", false},
		{"WI-a3f8", false},  // uppercase prefix
		{"a3f8", false},     // no prefix
		{"feat-123", false}, // wrong prefix
		{"wia3f8", false},   // missing hyphen
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isWorkItemID(tt.input)
			if got != tt.want {
				t.Errorf("isWorkItemID(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
