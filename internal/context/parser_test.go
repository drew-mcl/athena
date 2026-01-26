package context

import "testing"

const expectedOneFindingFormat = "expected 1 finding, got %d"

func TestParserParseBlockFormats(t *testing.T) {
	p := NewParser()

	text := `Some output text

## DECISION: Use Repository Pattern
We will use the repository pattern for data access.
This provides better abstraction.

## FINDING: Legacy Code in auth module
The auth module has deprecated functions.
Reference: auth/legacy.go:45

## TRIED: Direct database queries
Outcome: Failed
Reason: Violates separation of concerns

## QUESTION: Should we migrate to v2 API?
Context: v1 API is deprecated but still works
`

	markers := p.Parse(text)

	// Count by type
	counts := make(map[MarkerType]int)
	for _, m := range markers {
		counts[m.MarkerType]++
	}

	if counts[MarkerTypeDecision] != 1 {
		t.Errorf("expected 1 decision, got %d", counts[MarkerTypeDecision])
	}
	if counts[MarkerTypeFinding] != 1 {
		t.Errorf(expectedOneFindingFormat, counts[MarkerTypeFinding])
	}
	if counts[MarkerTypeTried] != 1 {
		t.Errorf("expected 1 tried, got %d", counts[MarkerTypeTried])
	}
	if counts[MarkerTypeQuestion] != 1 {
		t.Errorf("expected 1 question, got %d", counts[MarkerTypeQuestion])
	}
}

func TestParserParseInlineFormats(t *testing.T) {
	p := NewParser()

	text := `I explored the codebase and [[DECISION: use the existing auth middleware]].
Found that [[FINDING: the config package already has validation helpers]].
Earlier I [[TRIED: direct file parsing but it was too slow]].
One thing remains unclear: [[QUESTION: should we cache parsed configs?]]`

	markers := p.Parse(text)

	counts := make(map[MarkerType]int)
	for _, m := range markers {
		counts[m.MarkerType]++
	}

	if counts[MarkerTypeDecision] != 1 {
		t.Errorf("expected 1 decision, got %d", counts[MarkerTypeDecision])
	}
	if counts[MarkerTypeFinding] != 1 {
		t.Errorf(expectedOneFindingFormat, counts[MarkerTypeFinding])
	}
	if counts[MarkerTypeTried] != 1 {
		t.Errorf("expected 1 tried, got %d", counts[MarkerTypeTried])
	}
	if counts[MarkerTypeQuestion] != 1 {
		t.Errorf("expected 1 question, got %d", counts[MarkerTypeQuestion])
	}
}

func TestParserParseMixedFormats(t *testing.T) {
	p := NewParser()

	text := `## DECISION: Main Architecture Choice
We're using microservices.

Also, [[DECISION: each service gets its own database]].

## FINDING: Performance Bottleneck
The API gateway is slow.
Reference: gateway/main.go:120

And I noticed [[FINDING: the cache layer is not being used]].
`

	markers := p.Parse(text)

	decisions := 0
	findings := 0
	for _, m := range markers {
		switch m.MarkerType {
		case MarkerTypeDecision:
			decisions++
		case MarkerTypeFinding:
			findings++
		}
	}

	if decisions != 2 {
		t.Errorf("expected 2 decisions (1 block + 1 inline), got %d", decisions)
	}
	if findings != 2 {
		t.Errorf("expected 2 findings (1 block + 1 inline), got %d", findings)
	}
}

func TestParserInlineContent(t *testing.T) {
	p := NewParser()

	text := `[[DECISION: use repository pattern for data access]]`

	markers := p.Parse(text)

	if len(markers) != 1 {
		t.Fatalf("expected 1 marker, got %d", len(markers))
	}

	m := markers[0]
	if m.MarkerType != MarkerTypeDecision {
		t.Errorf("expected decision type, got %s", m.MarkerType)
	}
	if m.Content != "use repository pattern for data access" {
		t.Errorf("unexpected content: %s", m.Content)
	}
	if m.Title != "" {
		t.Errorf("inline markers should have empty title, got: %s", m.Title)
	}
}

func TestParserHasMarkers(t *testing.T) {
	p := NewParser()

	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{"no markers", "Just some regular text", false},
		{"block decision", "## DECISION: something\ncontent\n", true},
		{"block finding", "## FINDING: something\ncontent\n", true},
		{"inline decision", "text [[DECISION: something]] more text", false}, // HasMarkers only checks block formats
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.HasMarkers(tt.text)
			if got != tt.expected {
				t.Errorf("HasMarkers() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParserMarkerCount(t *testing.T) {
	p := NewParser()

	text := `## DECISION: First
content

[[DECISION: Second]]

## FINDING: Third
content
`

	count := p.MarkerCount(text)
	if count != 3 {
		t.Errorf("expected 3 markers, got %d", count)
	}
}

func TestParserBlockFindingWithReference(t *testing.T) {
	p := NewParser()

	text := `## FINDING: Database Connection Issue
The connection pool is exhausted.
Reference: db/pool.go:42
`

	markers := p.ParseFindings(text)

	if len(markers) != 1 {
		t.Fatalf(expectedOneFindingFormat, len(markers))
	}

	m := markers[0]
	if m.Title != "Database Connection Issue" {
		t.Errorf("unexpected title: %s", m.Title)
	}
	if ref, ok := m.Metadata["reference"]; !ok || ref != "db/pool.go:42" {
		t.Errorf("reference not extracted correctly: %v", m.Metadata)
	}
}

func TestParserBlockTriedWithOutcomeAndReason(t *testing.T) {
	p := NewParser()

	text := `## TRIED: Caching with Redis
Outcome: Success
Reason: Reduced latency by 50%
`

	markers := p.ParseAttempts(text)

	if len(markers) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(markers))
	}

	m := markers[0]
	if m.Title != "Caching with Redis" {
		t.Errorf("unexpected title: %s", m.Title)
	}
	if outcome, ok := m.Metadata["outcome"]; !ok || outcome != "Success" {
		t.Errorf("outcome not extracted correctly: %v", m.Metadata)
	}
	if reason, ok := m.Metadata["reason"]; !ok || reason != "Reduced latency by 50%" {
		t.Errorf("reason not extracted correctly: %v", m.Metadata)
	}
}
