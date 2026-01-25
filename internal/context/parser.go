package context

import (
	"regexp"
	"strings"
)

// Parser extracts structured markers from agent output.
type Parser struct{}

// NewParser creates a new Parser instance.
func NewParser() *Parser {
	return &Parser{}
}

// Marker patterns for extraction
// Supports two formats:
// 1. Block format: ## DECISION: <title>\n<content>
// 2. Inline format: [[DECISION: <content>]]
var (
	// Block format: ## DECISION: <title>\n<content>
	decisionPattern = regexp.MustCompile(`(?m)^##\s*DECISION:\s*(.+)\n((?:.*\n)*?)(?:(?:^##\s)|(?:\z))`)

	// Block format: ## FINDING: <title>\n<content>\nReference: <ref>
	findingPattern = regexp.MustCompile(`(?m)^##\s*FINDING:\s*(.+)\n((?:.*\n)*?)(?:(?:^##\s)|(?:\z))`)

	// Block format: ## TRIED: <what>\nOutcome: <outcome>\nReason: <reason>
	triedPattern = regexp.MustCompile(`(?m)^##\s*TRIED:\s*(.+)\n((?:.*\n)*?)(?:(?:^##\s)|(?:\z))`)

	// Block format: ## QUESTION: <question>\nContext: <context>
	questionPattern = regexp.MustCompile(`(?m)^##\s*QUESTION:\s*(.+)\n((?:.*\n)*?)(?:(?:^##\s)|(?:\z))`)

	// Block format: ## STATE: <type>/<key>\n<value>
	statePattern = regexp.MustCompile(`(?m)^##\s*STATE:\s*(.+)\n((?:.*\n)*?)(?:(?:^##\s)|(?:\z))`)

	// Inline formats: [[MARKER: <content>]]
	inlineDecisionPattern = regexp.MustCompile(`\[\[DECISION:\s*(.+?)\]\]`)
	inlineFindingPattern  = regexp.MustCompile(`\[\[FINDING:\s*(.+?)\]\]`)
	inlineTriedPattern    = regexp.MustCompile(`\[\[TRIED:\s*(.+?)\]\]`)
	inlineQuestionPattern = regexp.MustCompile(`\[\[QUESTION:\s*(.+?)\]\]`)

	// Reference line extraction
	referencePattern = regexp.MustCompile(`(?m)^Reference:\s*(.+)$`)

	// Outcome line extraction
	outcomePattern = regexp.MustCompile(`(?m)^Outcome:\s*(.+)$`)

	// Reason line extraction
	reasonPattern = regexp.MustCompile(`(?m)^Reason:\s*(.+)$`)

	// Context line extraction
	contextPattern = regexp.MustCompile(`(?m)^Context:\s*(.+)$`)
)

// Parse extracts all markers from the given text.
func (p *Parser) Parse(text string) []*ParsedMarker {
	var markers []*ParsedMarker

	// Extract decisions
	markers = append(markers, p.extractDecisions(text)...)

	// Extract findings
	markers = append(markers, p.extractFindings(text)...)

	// Extract tried/attempts
	markers = append(markers, p.extractTried(text)...)

	// Extract questions
	markers = append(markers, p.extractQuestions(text)...)

	// Extract state updates
	markers = append(markers, p.extractState(text)...)

	return markers
}

// ParseDecisions extracts only decision markers.
func (p *Parser) ParseDecisions(text string) []*ParsedMarker {
	return p.extractDecisions(text)
}

// ParseFindings extracts only finding markers.
func (p *Parser) ParseFindings(text string) []*ParsedMarker {
	return p.extractFindings(text)
}

// ParseAttempts extracts only tried/attempt markers.
func (p *Parser) ParseAttempts(text string) []*ParsedMarker {
	return p.extractTried(text)
}

// ParseQuestions extracts only question markers.
func (p *Parser) ParseQuestions(text string) []*ParsedMarker {
	return p.extractQuestions(text)
}

// ParseState extracts only state markers.
func (p *Parser) ParseState(text string) []*ParsedMarker {
	return p.extractState(text)
}

func (p *Parser) extractDecisions(text string) []*ParsedMarker {
	var markers []*ParsedMarker

	// Block format: ## DECISION: <title>\n<content>
	matches := decisionPattern.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		if len(match) >= 3 {
			title := strings.TrimSpace(match[1])
			content := strings.TrimSpace(match[2])

			markers = append(markers, &ParsedMarker{
				MarkerType: MarkerTypeDecision,
				Title:      title,
				Content:    content,
				Metadata:   make(map[string]string),
			})
		}
	}

	// Inline format: [[DECISION: <content>]]
	inlineMatches := inlineDecisionPattern.FindAllStringSubmatch(text, -1)
	for _, match := range inlineMatches {
		if len(match) >= 2 {
			content := strings.TrimSpace(match[1])
			markers = append(markers, &ParsedMarker{
				MarkerType: MarkerTypeDecision,
				Title:      "",
				Content:    content,
				Metadata:   make(map[string]string),
			})
		}
	}

	return markers
}

func (p *Parser) extractFindings(text string) []*ParsedMarker {
	var markers []*ParsedMarker

	// Block format: ## FINDING: <title>\n<content>
	matches := findingPattern.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		if len(match) >= 3 {
			title := strings.TrimSpace(match[1])
			content := strings.TrimSpace(match[2])
			metadata := make(map[string]string)

			// Extract reference if present
			if refMatch := referencePattern.FindStringSubmatch(content); len(refMatch) >= 2 {
				metadata["reference"] = strings.TrimSpace(refMatch[1])
				// Remove reference line from content
				content = referencePattern.ReplaceAllString(content, "")
				content = strings.TrimSpace(content)
			}

			markers = append(markers, &ParsedMarker{
				MarkerType: MarkerTypeFinding,
				Title:      title,
				Content:    content,
				Metadata:   metadata,
			})
		}
	}

	// Inline format: [[FINDING: <content>]]
	inlineMatches := inlineFindingPattern.FindAllStringSubmatch(text, -1)
	for _, match := range inlineMatches {
		if len(match) >= 2 {
			content := strings.TrimSpace(match[1])
			markers = append(markers, &ParsedMarker{
				MarkerType: MarkerTypeFinding,
				Title:      "",
				Content:    content,
				Metadata:   make(map[string]string),
			})
		}
	}

	return markers
}

func (p *Parser) extractTried(text string) []*ParsedMarker {
	var markers []*ParsedMarker

	// Block format: ## TRIED: <title>\n<content>
	matches := triedPattern.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		if len(match) >= 3 {
			title := strings.TrimSpace(match[1])
			content := strings.TrimSpace(match[2])
			metadata := make(map[string]string)

			// Extract outcome if present
			if outcomeMatch := outcomePattern.FindStringSubmatch(content); len(outcomeMatch) >= 2 {
				metadata["outcome"] = strings.TrimSpace(outcomeMatch[1])
				content = outcomePattern.ReplaceAllString(content, "")
			}

			// Extract reason if present
			if reasonMatch := reasonPattern.FindStringSubmatch(content); len(reasonMatch) >= 2 {
				metadata["reason"] = strings.TrimSpace(reasonMatch[1])
				content = reasonPattern.ReplaceAllString(content, "")
			}

			content = strings.TrimSpace(content)

			markers = append(markers, &ParsedMarker{
				MarkerType: MarkerTypeTried,
				Title:      title,
				Content:    content,
				Metadata:   metadata,
			})
		}
	}

	// Inline format: [[TRIED: <content>]]
	inlineMatches := inlineTriedPattern.FindAllStringSubmatch(text, -1)
	for _, match := range inlineMatches {
		if len(match) >= 2 {
			content := strings.TrimSpace(match[1])
			markers = append(markers, &ParsedMarker{
				MarkerType: MarkerTypeTried,
				Title:      "",
				Content:    content,
				Metadata:   make(map[string]string),
			})
		}
	}

	return markers
}

func (p *Parser) extractQuestions(text string) []*ParsedMarker {
	var markers []*ParsedMarker

	// Block format: ## QUESTION: <title>\n<content>
	matches := questionPattern.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		if len(match) >= 3 {
			title := strings.TrimSpace(match[1])
			content := strings.TrimSpace(match[2])
			metadata := make(map[string]string)

			// Extract context if present
			if ctxMatch := contextPattern.FindStringSubmatch(content); len(ctxMatch) >= 2 {
				metadata["context"] = strings.TrimSpace(ctxMatch[1])
				content = contextPattern.ReplaceAllString(content, "")
				content = strings.TrimSpace(content)
			}

			markers = append(markers, &ParsedMarker{
				MarkerType: MarkerTypeQuestion,
				Title:      title,
				Content:    content,
				Metadata:   metadata,
			})
		}
	}

	// Inline format: [[QUESTION: <content>]]
	inlineMatches := inlineQuestionPattern.FindAllStringSubmatch(text, -1)
	for _, match := range inlineMatches {
		if len(match) >= 2 {
			content := strings.TrimSpace(match[1])
			markers = append(markers, &ParsedMarker{
				MarkerType: MarkerTypeQuestion,
				Title:      "",
				Content:    content,
				Metadata:   make(map[string]string),
			})
		}
	}

	return markers
}

func (p *Parser) extractState(text string) []*ParsedMarker {
	var markers []*ParsedMarker

	matches := statePattern.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		if len(match) >= 3 {
			// Title format: <type>/<key>
			title := strings.TrimSpace(match[1])
			content := strings.TrimSpace(match[2])
			metadata := make(map[string]string)

			// Parse type and key from title
			parts := strings.SplitN(title, "/", 2)
			if len(parts) == 2 {
				metadata["state_type"] = strings.TrimSpace(parts[0])
				metadata["key"] = strings.TrimSpace(parts[1])
			}

			markers = append(markers, &ParsedMarker{
				MarkerType: MarkerTypeState,
				Title:      title,
				Content:    content,
				Metadata:   metadata,
			})
		}
	}

	return markers
}

// HasMarkers returns true if the text contains any markers.
func (p *Parser) HasMarkers(text string) bool {
	patterns := []*regexp.Regexp{
		decisionPattern,
		findingPattern,
		triedPattern,
		questionPattern,
		statePattern,
	}

	for _, pat := range patterns {
		if pat.MatchString(text) {
			return true
		}
	}
	return false
}

// MarkerCount returns the total number of markers in the text.
func (p *Parser) MarkerCount(text string) int {
	return len(p.Parse(text))
}

// FormatForBlackboard converts a parsed marker to blackboard content string.
func (p *Parser) FormatForBlackboard(marker *ParsedMarker) string {
	var sb strings.Builder

	// Title first
	sb.WriteString(marker.Title)

	// Add content if non-empty
	if marker.Content != "" {
		sb.WriteString("\n")
		sb.WriteString(marker.Content)
	}

	// Add relevant metadata
	switch marker.MarkerType {
	case MarkerTypeFinding:
		if ref, ok := marker.Metadata["reference"]; ok && ref != "" {
			sb.WriteString("\nReference: ")
			sb.WriteString(ref)
		}
	case MarkerTypeTried:
		if outcome, ok := marker.Metadata["outcome"]; ok && outcome != "" {
			sb.WriteString("\nOutcome: ")
			sb.WriteString(outcome)
		}
		if reason, ok := marker.Metadata["reason"]; ok && reason != "" {
			sb.WriteString("\nReason: ")
			sb.WriteString(reason)
		}
	case MarkerTypeQuestion:
		if ctx, ok := marker.Metadata["context"]; ok && ctx != "" {
			sb.WriteString("\nContext: ")
			sb.WriteString(ctx)
		}
	}

	return sb.String()
}

// MapMarkerTypeToBlackboard maps a marker type to a blackboard entry type.
func MapMarkerTypeToBlackboard(mt MarkerType) (BlackboardEntryType, bool) {
	switch mt {
	case MarkerTypeDecision:
		return BlackboardTypeDecision, true
	case MarkerTypeFinding:
		return BlackboardTypeFinding, true
	case MarkerTypeTried:
		return BlackboardTypeAttempt, true
	case MarkerTypeQuestion:
		return BlackboardTypeQuestion, true
	default:
		return "", false
	}
}

// MapMarkerTypeToState maps a state marker's type metadata to a state entry type.
func MapMarkerTypeToState(typeStr string) (StateEntryType, bool) {
	switch strings.ToLower(typeStr) {
	case "architecture", "arch":
		return StateTypeArchitecture, true
	case "convention", "conv":
		return StateTypeConvention, true
	case "constraint":
		return StateTypeConstraint, true
	case "decision":
		return StateTypeDecision, true
	case "environment", "env":
		return StateTypeEnvironment, true
	default:
		return "", false
	}
}
