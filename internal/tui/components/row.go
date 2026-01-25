package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/drewfead/athena/internal/tui"
)

// MultiLineRow renders a multi-line list item with selection indicator.
type MultiLineRow struct {
	Lines      []string
	Selected   bool
	Indicator  string // Selection indicator (e.g., "▌")
	LineHeight int    // Lines per row for consistent spacing
}

// NewMultiLineRow creates a multi-line row.
func NewMultiLineRow(lines ...string) *MultiLineRow {
	return &MultiLineRow{
		Lines:      lines,
		Selected:   false,
		Indicator:  "▌",
		LineHeight: len(lines),
	}
}

// WithSelected marks the row as selected.
func (r *MultiLineRow) WithSelected(selected bool) *MultiLineRow {
	r.Selected = selected
	return r
}

// WithIndicator sets the selection indicator character.
func (r *MultiLineRow) WithIndicator(ind string) *MultiLineRow {
	r.Indicator = ind
	return r
}

// Render returns the styled row string.
func (r *MultiLineRow) Render() string {
	var sb strings.Builder

	indicatorStyle := lipgloss.NewStyle().Foreground(tui.ColorAccent).Bold(true)
	emptyIndicator := strings.Repeat(" ", lipgloss.Width(r.Indicator))

	for i, line := range r.Lines {
		// First line gets indicator if selected
		if i == 0 && r.Selected {
			sb.WriteString(indicatorStyle.Render(r.Indicator))
			sb.WriteString(" ")
		} else {
			sb.WriteString(emptyIndicator)
			sb.WriteString(" ")
		}

		// Apply selection styling to first line
		if r.Selected && i == 0 {
			lineStyle := lipgloss.NewStyle().Bold(true)
			sb.WriteString(lineStyle.Render(line))
		} else {
			sb.WriteString(line)
		}

		if i < len(r.Lines)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// WorktreeRow renders a worktree with branch, summary, and metadata.
type WorktreeRow struct {
	Branch     string
	Summary    string
	Project    string
	GitStatus  string // "clean", "dirty"
	LastActive string
	Status     string // "RUNNING", "IDLE", "MERGED"
	AgentCount int
	Selected   bool
	Width      int
}

// Render returns the styled worktree row.
func (w *WorktreeRow) Render() string {
	var lines []string

	// Line 1: Branch (left) + Status pill (right)
	line1 := w.renderLine1()
	lines = append(lines, line1)

	// Line 2: Summary
	line2 := w.renderLine2()
	lines = append(lines, line2)

	// Line 3: Project • git status • agents • time
	line3 := w.renderLine3()
	lines = append(lines, line3)

	// Blank line for spacing between items
	lines = append(lines, "")

	return NewMultiLineRow(lines...).WithSelected(w.Selected).Render()
}

func (w *WorktreeRow) renderLine1() string {
	// Calculate usable width (account for selection indicator)
	usableWidth := w.Width - 4
	if usableWidth < 40 {
		usableWidth = 40
	}

	// Branch name
	branchStyle := lipgloss.NewStyle().Foreground(tui.ColorFg).Bold(true)
	branchText := branchStyle.Render(w.Branch)

	// Status pill
	pillStyle, pillText := w.statusPill()
	pill := pillStyle.Render("[ " + pillText + " ]")

	// Right-align status pill
	branchLen := lipgloss.Width(branchText)
	pillLen := lipgloss.Width(pill)
	padding := usableWidth - branchLen - pillLen
	if padding < 2 {
		padding = 2
	}

	return branchText + strings.Repeat(" ", padding) + pill
}

func (w *WorktreeRow) renderLine2() string {
	// Summary (truncated based on width)
	summaryStyle := lipgloss.NewStyle().Foreground(tui.ColorFgSecondary)
	summary := w.Summary
	maxLen := w.Width - 8
	if maxLen < 30 {
		maxLen = 30
	}
	if len(summary) > maxLen {
		summary = summary[:maxLen-3] + "..."
	}
	return summaryStyle.Render(summary)
}

func (w *WorktreeRow) renderLine3() string {
	mutedStyle := lipgloss.NewStyle().Foreground(tui.ColorFgMuted)

	parts := []string{w.Project}

	// Git status with color
	if w.GitStatus == "dirty" {
		warningStyle := lipgloss.NewStyle().Foreground(tui.ColorWarning)
		parts = append(parts, warningStyle.Render("dirty"))
	} else {
		successStyle := lipgloss.NewStyle().Foreground(tui.ColorSuccess)
		parts = append(parts, successStyle.Render("clean"))
	}

	// Agent info
	if w.AgentCount > 0 {
		agentStyle := lipgloss.NewStyle().Foreground(tui.ColorInfo)
		agentText := itoa(w.AgentCount) + " agent"
		if w.AgentCount != 1 {
			agentText += "s"
		}
		parts = append(parts, agentStyle.Render(agentText))
	}

	// Last active
	if w.LastActive != "" {
		parts = append(parts, w.LastActive)
	}

	return mutedStyle.Render(strings.Join(parts, " • "))
}

func (w *WorktreeRow) statusPill() (lipgloss.Style, string) {
	// Fixed width status for alignment
	format := func(s string) string {
		// Pad to 9 chars for consistent pill width
		for len(s) < 9 {
			s = s + " "
		}
		return s
	}
	switch w.Status {
	case "RUNNING":
		return lipgloss.NewStyle().Foreground(tui.ColorSuccess), format("RUNNING")
	case "MERGED":
		return lipgloss.NewStyle().Foreground(tui.ColorNeutral), format("MERGED")
	case "ATTENTION":
		return lipgloss.NewStyle().Foreground(tui.ColorWarning), format("ATTENTION")
	default:
		return lipgloss.NewStyle().Foreground(tui.ColorFgMuted), format("IDLE")
	}
}

// AgentRow renders an agent with status, progress, and actions.
type AgentRow struct {
	ID         string
	Status     string // "running", "planning", "awaiting", etc.
	Archetype  string
	Worktree   string // "project/branch"
	Activity   string // Current activity description
	Duration   string // Time elapsed
	TokensUsed int
	TokensMax  int
	CacheRate  float64
	Selected   bool
}

// Render returns the styled agent row.
func (a *AgentRow) Render() string {
	var lines []string

	// Line 1: Status icon + ID + archetype + worktree
	line1 := a.renderLine1()
	lines = append(lines, line1)

	// Line 2: Activity + duration
	line2 := a.renderLine2()
	lines = append(lines, line2)

	// Line 3: Token progress bar
	line3 := a.renderLine3()
	lines = append(lines, line3)

	// Line 4: Action hints for awaiting agents
	if a.Status == "awaiting" {
		line4 := a.renderLine4()
		lines = append(lines, line4)
	}

	return NewMultiLineRow(lines...).WithSelected(a.Selected).Render()
}

func (a *AgentRow) renderLine1() string {
	var sb strings.Builder

	// Status icon
	icon := tui.StatusIcons[a.Status]
	if icon == "" {
		icon = "-"
	}
	iconStyle := tui.StatusStyle(a.Status)
	sb.WriteString(iconStyle.Render(icon))
	sb.WriteString(" ")

	// Agent ID (short)
	idStyle := lipgloss.NewStyle().Foreground(tui.ColorFgSecondary)
	id := a.ID
	if len(id) > 6 {
		id = id[:6]
	}
	sb.WriteString(idStyle.Render(id))
	sb.WriteString("  ")

	// Archetype
	archeStyle := lipgloss.NewStyle().Foreground(tui.ColorFg)
	sb.WriteString(archeStyle.Render(a.Archetype))
	sb.WriteString("  ")

	// Worktree
	wtStyle := lipgloss.NewStyle().Foreground(tui.ColorFgMuted)
	sb.WriteString(wtStyle.Render(a.Worktree))

	return sb.String()
}

func (a *AgentRow) renderLine2() string {
	var sb strings.Builder

	// Activity
	actStyle := lipgloss.NewStyle().Foreground(tui.ColorFgSecondary)
	activity := a.Activity
	if len(activity) > 45 {
		activity = activity[:42] + "..."
	}
	sb.WriteString(actStyle.Render(activity))

	// Duration (right side)
	if a.Duration != "" {
		sb.WriteString("  ")
		durStyle := lipgloss.NewStyle().Foreground(tui.ColorFgMuted)
		sb.WriteString(durStyle.Render(a.Duration))
	}

	return sb.String()
}

func (a *AgentRow) renderLine3() string {
	progress := NewTokenProgress(a.TokensUsed, a.TokensMax, a.CacheRate).WithWidth(20)
	return progress.Render()
}

func (a *AgentRow) renderLine4() string {
	hintStyle := lipgloss.NewStyle().Foreground(tui.ColorWarning)
	return hintStyle.Render("Press [p] to view plan, [A] to approve")
}

// TaskRow renders a task with status and dependencies.
type TaskRow struct {
	Subject     string
	Status      string // "pending", "in_progress", "completed"
	Age         string
	BlockedBy   []string
	Selected    bool
}

// Render returns the styled task row.
func (t *TaskRow) Render() string {
	var lines []string

	// Line 1: Status icon + subject
	line1 := t.renderLine1()
	lines = append(lines, line1)

	// Line 2: Status + age/blockers
	line2 := t.renderLine2()
	lines = append(lines, line2)

	return NewMultiLineRow(lines...).WithSelected(t.Selected).Render()
}

func (t *TaskRow) renderLine1() string {
	var sb strings.Builder

	// Status indicator (text-based)
	var icon string
	var iconStyle lipgloss.Style
	switch t.Status {
	case "in_progress":
		icon = "*"
		iconStyle = lipgloss.NewStyle().Foreground(tui.ColorSuccess)
	case "completed":
		icon = "+"
		iconStyle = lipgloss.NewStyle().Foreground(tui.ColorNeutral)
	default:
		icon = "o"
		iconStyle = lipgloss.NewStyle().Foreground(tui.ColorFgMuted)
	}
	sb.WriteString(iconStyle.Render(icon))
	sb.WriteString(" ")

	// Subject
	subjectStyle := lipgloss.NewStyle().Foreground(tui.ColorFg)
	if t.Status == "completed" {
		subjectStyle = subjectStyle.Foreground(tui.ColorFgMuted)
	}
	sb.WriteString(subjectStyle.Render(t.Subject))

	return sb.String()
}

func (t *TaskRow) renderLine2() string {
	var sb strings.Builder
	mutedStyle := lipgloss.NewStyle().Foreground(tui.ColorFgMuted)

	sb.WriteString(mutedStyle.Render(t.Status))

	if len(t.BlockedBy) > 0 {
		sb.WriteString(" • ")
		blockStyle := lipgloss.NewStyle().Foreground(tui.ColorWarning)
		sb.WriteString(blockStyle.Render("Blocked by: " + strings.Join(t.BlockedBy, ", ")))
	} else if t.Age != "" {
		sb.WriteString(" • ")
		sb.WriteString(mutedStyle.Render(t.Age))
	}

	return sb.String()
}

// QuestionRow renders a question with answer preview.
type QuestionRow struct {
	Question   string
	Answer     string // Empty if pending
	Status     string // "answered", "pending"
	Age        string
	Selected   bool
	Width      int // Available width for content
}

// Render returns the styled question row.
func (q *QuestionRow) Render() string {
	var lines []string

	// Line 1: Status icon + question
	line1 := q.renderLine1()
	lines = append(lines, line1)

	// Line 2: Answer preview or spinner + age
	line2 := q.renderLine2()
	lines = append(lines, line2)

	return NewMultiLineRow(lines...).WithSelected(q.Selected).Render()
}

func (q *QuestionRow) maxTextWidth() int {
	// Default to 80 if not specified
	width := q.Width
	if width == 0 {
		width = 80
	}
	// Account for: indicator (4), icon (2), quotes (2), padding (4)
	return width - 12
}

func (q *QuestionRow) renderLine1() string {
	var sb strings.Builder

	// Status indicator (text-based)
	var icon string
	var iconStyle lipgloss.Style
	if q.Status == "answered" {
		icon = "+"
		iconStyle = lipgloss.NewStyle().Foreground(tui.ColorSuccess)
	} else {
		icon = "*"
		iconStyle = lipgloss.NewStyle().Foreground(tui.ColorWarning)
	}
	sb.WriteString(iconStyle.Render(icon))
	sb.WriteString(" ")

	// Question (quoted, truncated based on available width)
	questionStyle := lipgloss.NewStyle().Foreground(tui.ColorFg)
	question := q.Question
	maxLen := q.maxTextWidth()
	if maxLen < 20 {
		maxLen = 20
	}
	if len(question) > maxLen {
		question = question[:maxLen-3] + "..."
	}
	sb.WriteString(questionStyle.Render("\"" + question + "\""))

	return sb.String()
}

func (q *QuestionRow) renderLine2() string {
	var sb strings.Builder

	// Reserve space for age (approx 10 chars)
	maxLen := q.maxTextWidth() - 10
	if maxLen < 20 {
		maxLen = 20
	}

	if q.Status == "answered" {
		// Answer preview
		answerStyle := lipgloss.NewStyle().Foreground(tui.ColorFgSecondary)
		answer := q.Answer
		if len(answer) > maxLen {
			answer = answer[:maxLen-3] + "..."
		}
		sb.WriteString(answerStyle.Render(answer))
	} else {
		// Spinner
		spinnerStyle := lipgloss.NewStyle().Foreground(tui.ColorWarning)
		sb.WriteString(spinnerStyle.Render("Thinking..."))
	}

	// Age
	if q.Age != "" {
		sb.WriteString("  ")
		ageStyle := lipgloss.NewStyle().Foreground(tui.ColorFgMuted)
		sb.WriteString(ageStyle.Render(q.Age))
	}

	return sb.String()
}
