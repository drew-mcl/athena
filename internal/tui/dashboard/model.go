// Package dashboard provides the main TUI dashboard view.
package dashboard

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/terminal"
	"github.com/drewfead/athena/internal/tui"
)

// Level represents navigation depth
type Level int

const (
	LevelDashboard Level = iota // Global view - all worktrees/agents/jobs
	LevelProject                // Inside a project - project-specific details
)

// Tab represents which tab is active within a project
type Tab int

const (
	TabWorktrees Tab = iota
	TabJobs
	TabAgents
	TabPlans
	TabNotes
	TabChangelog
)

// Model is the main dashboard model
type Model struct {
	// Data
	projects  []string // Unique project names
	worktrees []*control.WorktreeInfo
	jobs      []*control.JobInfo
	agents    []*control.AgentInfo
	notes     []*control.NoteInfo      // Quick notes/ideas
	changelog []*control.ChangelogInfo // Completed work history

	// Navigation
	level          Level
	selectedProject string
	tab            Tab
	selected       int

	// UI state
	width        int
	height       int
	inputMode    bool
	questionMode bool // true = question, false = job
	noteMode     bool // true = adding note
	detailMode    bool // showing detail view
	detailJob     *control.JobInfo
	detailAgent   *control.AgentInfo
	logsMode      bool // showing agent logs
	logsAgentID  string
	logs         []*control.AgentEventInfo
	textInput    textinput.Model
	spinner      spinner.Model
	lastUpdate   time.Time
	err          error

	// Client connection
	client *control.Client

	// Terminal integration
	term terminal.Terminal
}

// Messages
type (
	tickMsg            time.Time
	dataUpdateMsg      struct{}
	errMsg             error
	eventMsg           control.Event
	fetchDataResultMsg struct {
		worktrees []*control.WorktreeInfo
		agents    []*control.AgentInfo
		jobs      []*control.JobInfo
		notes     []*control.NoteInfo
		changelog []*control.ChangelogInfo
	}
	logsResultMsg struct {
		agentID string
		logs    []*control.AgentEventInfo
	}
)

// New creates a new dashboard model
func New(client *control.Client) Model {
	ti := textinput.New()
	ti.Placeholder = "Enter task description..."
	ti.CharLimit = 500
	ti.Width = 60

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(tui.ColorAccent)

	return Model{
		client:    client,
		level:     LevelDashboard,
		tab:       TabWorktrees,
		spinner:   sp,
		textInput: ti,
		term:      terminal.Detect(),
	}
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.fetchData,
		m.tick(),
		m.listenForEvents(),
	)
}

// Update implements tea.Model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.inputMode {
			return m.handleInputMode(msg)
		}
		if m.logsMode {
			return m.handleLogsMode(msg)
		}
		if m.detailMode {
			return m.handleDetailMode(msg)
		}
		return m.handleNormalMode(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		return m, tea.Batch(m.fetchData, m.tick())

	case fetchDataResultMsg:
		m.worktrees = msg.worktrees
		m.agents = msg.agents
		m.jobs = msg.jobs
		m.notes = msg.notes
		m.changelog = msg.changelog
		m.projects = m.extractProjects()
		m.lastUpdate = time.Now()

	case errMsg:
		m.err = msg

	case dataUpdateMsg:
		// Data was modified (create/update/delete), refresh
		cmds = append(cmds, m.fetchData)

	case eventMsg:
		cmds = append(cmds, m.fetchData)

	case logsResultMsg:
		m.logsAgentID = msg.agentID
		m.logs = msg.logs
		m.logsMode = true

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) extractProjects() []string {
	seen := make(map[string]bool)
	var projects []string
	for _, wt := range m.worktrees {
		if !seen[wt.Project] {
			seen[wt.Project] = true
			projects = append(projects, wt.Project)
		}
	}
	return projects
}

func (m Model) handleNormalMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		if m.level == LevelProject {
			// Go back to projects
			m.level = LevelDashboard
			m.selected = 0
			m.selectedProject = ""
			return m, nil
		}
		return m, tea.Quit

	case "ctrl+c":
		return m, tea.Quit

	case "esc":
		if m.level == LevelProject {
			m.level = LevelDashboard
			m.selected = 0
			m.selectedProject = ""
			return m, nil
		}

	case "enter":
		if m.level == LevelDashboard {
			// Drill into project based on current tab selection
			var project string
			switch m.tab {
			case TabWorktrees:
				if m.selected < len(m.worktrees) {
					project = m.worktrees[m.selected].Project
				}
			case TabJobs:
				if m.selected < len(m.jobs) {
					// Show job detail at dashboard level
					m.detailJob = m.jobs[m.selected]
					m.detailMode = true
					return m, nil
				}
			case TabAgents:
				if m.selected < len(m.agents) {
					// Show agent detail at dashboard level
					m.detailAgent = m.agents[m.selected]
					m.detailMode = true
					return m, nil
				}
			case TabNotes:
				// Notes don't drill down
				return m, nil
			}
			if project != "" {
				m.selectedProject = project
				m.level = LevelProject
				m.selected = 0
				return m, nil
			}
		}
		if m.level == LevelProject && m.tab == TabJobs {
			// Show job detail
			jobs := m.projectJobs()
			if m.selected < len(jobs) {
				m.detailJob = jobs[m.selected]
				m.detailMode = true
				return m, nil
			}
		}

	case "tab", "l":
		// Tab works at both levels
		if m.level == LevelDashboard {
			m.tab = (m.tab + 1) % 5 // worktrees, jobs, agents, notes, changelog at dashboard
			m.selected = 0
		} else {
			m.tab = (m.tab + 1) % 6
			m.selected = 0
		}

	case "shift+tab", "h":
		if m.level == LevelDashboard {
			m.tab = (m.tab + 4) % 5
			m.selected = 0
		} else {
			m.tab = (m.tab + 5) % 6
			m.selected = 0
		}

	case "1", "2", "3", "4", "5", "6":
		switch msg.String() {
		case "1":
			m.tab = TabWorktrees
		case "2":
			m.tab = TabJobs
		case "3":
			m.tab = TabAgents
		case "4":
			if m.level == LevelDashboard {
				m.tab = TabNotes
			} else {
				m.tab = TabPlans
			}
		case "5":
			if m.level == LevelDashboard {
				m.tab = TabChangelog
			} else {
				m.tab = TabNotes
			}
		case "6":
			if m.level == LevelProject {
				m.tab = TabChangelog
			}
		}
		m.selected = 0

	case "j", "down":
		m.selected++
		m.clampSelection()

	case "k", "up":
		m.selected--
		m.clampSelection()

	case "n":
		// New note on notes tab (at any level)
		if m.tab == TabNotes {
			m.inputMode = true
			m.noteMode = true
			m.questionMode = false
			m.textInput.Placeholder = "Enter note..."
			m.textInput.Focus()
			return m, textinput.Blink
		}
		// New job/question at any level (except notes tab)
		m.inputMode = true
		m.questionMode = false
		m.noteMode = false
		m.textInput.Placeholder = "Enter task description..."
		m.textInput.Focus()
		return m, textinput.Blink

	case "N":
		// Capital N for normalize repos
		return m, m.doNormalize()

	case "?":
		// Quick question at any level
		m.inputMode = true
		m.questionMode = true
		m.noteMode = false
		m.textInput.Placeholder = "Ask a question..."
		m.textInput.Focus()
		return m, textinput.Blink

	case "a":
		m.doAttach()

	case "e":
		m.doOpenNvim()

	case "v":
		m.doView()

	case "L":
		// View logs for selected agent
		return m, m.doLogs()

	case "s":
		m.doShell()

	case "x", "ctrl+k":
		// Toggle note done on notes tab (at any level)
		if m.tab == TabNotes {
			if m.selected < len(m.notes) {
				note := m.notes[m.selected]
				return m, m.toggleNote(note.ID, !note.Done)
			}
			return m, nil
		}
		return m, m.doKill()

	case " ":
		// Space also toggles note done (at any level)
		if m.tab == TabNotes {
			if m.selected < len(m.notes) {
				note := m.notes[m.selected]
				return m, m.toggleNote(note.ID, !note.Done)
			}
		}

	case "d":
		// Delete note or changelog entry (at any level)
		if m.tab == TabNotes {
			if m.selected < len(m.notes) {
				note := m.notes[m.selected]
				return m, m.deleteNote(note.ID)
			}
		}
		if m.tab == TabChangelog {
			entries := m.changelog
			if m.level == LevelProject {
				entries = m.projectChangelog()
			}
			if m.selected < len(entries) {
				entry := entries[m.selected]
				return m, m.deleteChangelog(entry.ID)
			}
		}

	case "r":
		return m, m.fetchData
	}

	return m, nil
}

func (m Model) handleInputMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.inputMode = false
		m.noteMode = false
		m.textInput.Reset()
		return m, nil

	case "enter":
		input := m.textInput.Value()
		if input != "" {
			if m.noteMode {
				// Add note via API
				m.inputMode = false
				m.noteMode = false
				m.textInput.Reset()
				return m, m.createNote(input)
			}
			isQuestion := m.questionMode
			m.inputMode = false
			m.questionMode = false
			m.textInput.Reset()
			return m, m.createJob(input, isQuestion)
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m Model) handleDetailMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.detailMode = false
		m.detailJob = nil
		m.detailAgent = nil
		return m, nil

	// Actions available in agent detail
	case "L":
		if m.detailAgent != nil {
			return m, m.fetchLogs(m.detailAgent.ID)
		}
	case "a":
		if m.detailAgent != nil {
			m.term.AttachToAgent(m.detailAgent.WorktreePath, m.detailAgent.ID)
		}
	case "e":
		if m.detailAgent != nil {
			m.term.OpenNvim(m.detailAgent.WorktreePath, true)
		}
	case "s":
		if m.detailAgent != nil {
			m.term.OpenShell(m.detailAgent.WorktreePath)
		}
	case "x":
		if m.detailAgent != nil {
			agentID := m.detailAgent.ID
			m.detailMode = false
			m.detailAgent = nil
			return m, m.killAgent(agentID)
		}
	}
	return m, nil
}

func (m Model) handleLogsMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.logsMode = false
		m.logs = nil
		m.logsAgentID = ""
		return m, nil
	case "r":
		// Refresh logs
		return m, m.fetchLogs(m.logsAgentID)
	}
	return m, nil
}

func (m *Model) clampSelection() {
	maxIdx := m.getMaxSelection()
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected > maxIdx {
		m.selected = maxIdx
	}
}

func (m Model) getMaxSelection() int {
	if m.level == LevelDashboard {
		// Dashboard level - global lists
		switch m.tab {
		case TabWorktrees:
			return len(m.worktrees) - 1
		case TabJobs:
			return len(m.jobs) - 1
		case TabAgents:
			return len(m.agents) - 1
		case TabNotes:
			return len(m.notes) - 1
		case TabChangelog:
			return len(m.changelog) - 1
		}
		return 0
	}

	// Project level - filtered lists
	switch m.tab {
	case TabWorktrees:
		return len(m.projectWorktrees()) - 1
	case TabJobs:
		return len(m.projectJobs()) - 1
	case TabAgents:
		return len(m.projectAgents()) - 1
	case TabPlans:
		return len(m.projectPlans()) - 1
	case TabNotes:
		return len(m.notes) - 1
	case TabChangelog:
		return len(m.projectChangelog()) - 1
	}
	return 0
}

// Filtered data for current project
func (m Model) projectWorktrees() []*control.WorktreeInfo {
	var result []*control.WorktreeInfo
	for _, wt := range m.worktrees {
		if wt.Project == m.selectedProject {
			result = append(result, wt)
		}
	}
	return result
}

func (m Model) projectJobs() []*control.JobInfo {
	var result []*control.JobInfo
	for _, j := range m.jobs {
		if j.Project == m.selectedProject {
			result = append(result, j)
		}
	}
	return result
}

func (m Model) projectAgents() []*control.AgentInfo {
	var result []*control.AgentInfo
	for _, a := range m.agents {
		if a.Project == m.selectedProject {
			result = append(result, a)
		}
	}
	return result
}

func (m Model) projectChangelog() []*control.ChangelogInfo {
	var result []*control.ChangelogInfo
	for _, c := range m.changelog {
		if c.Project == m.selectedProject {
			result = append(result, c)
		}
	}
	return result
}

// Plan represents an agent's plan file
type Plan struct {
	AgentID   string
	Path      string
	Title     string
	CreatedAt time.Time
}

func (m Model) projectPlans() []Plan {
	// TODO: Load actual plan files from agents
	// For now, derive from agents that have plans
	var plans []Plan
	for _, a := range m.projectAgents() {
		if a.Status == "planning" || a.Status == "awaiting" {
			plans = append(plans, Plan{
				AgentID: a.ID,
				Path:    filepath.Join(a.WorktreePath, ".claude/plans"),
				Title:   fmt.Sprintf("Plan from %s", a.Archetype),
			})
		}
	}
	return plans
}

// View implements tea.Model
func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	// Detail overlays
	if m.detailMode {
		if m.detailJob != nil {
			return m.renderJobDetail()
		}
		if m.detailAgent != nil {
			return m.renderAgentDetail()
		}
	}

	// Logs overlay
	if m.logsMode {
		return m.renderLogs()
	}

	var b strings.Builder

	if m.level == LevelDashboard {
		b.WriteString(m.renderDashboard())
	} else {
		b.WriteString(m.renderProjectDetail())
	}

	return b.String()
}

func (m Model) renderDashboard() string {
	var b strings.Builder

	// Header with icon, tagline, and tabs
	icon := tui.StyleAccent.Render("▐▛▜▌")
	title := tui.StyleAccent.Bold(true).Render(" athena")

	// Tabs at dashboard level (worktrees, jobs, agents, notes, changelog)
	tabs := []struct {
		name string
		tab  Tab
	}{
		{"worktrees", TabWorktrees},
		{"jobs", TabJobs},
		{"agents", TabAgents},
		{"notes", TabNotes},
		{"changelog", TabChangelog},
	}

	var tabParts []string
	for _, t := range tabs {
		if t.tab == m.tab {
			tabParts = append(tabParts, tui.StyleAccent.Render(t.name))
		} else {
			tabParts = append(tabParts, tui.StyleMuted.Render(t.name))
		}
	}
	tabStr := strings.Join(tabParts, "  ")

	// Stats
	activeCount := 0
	for _, a := range m.agents {
		if a.Status == "running" || a.Status == "planning" || a.Status == "executing" {
			activeCount++
		}
	}
	stats := tui.StyleMuted.Render(fmt.Sprintf("%d/%d agents", activeCount, len(m.agents)))

	// Layout
	sep := tui.StyleMuted.Render(" │ ")
	left := icon + title + sep + tabStr
	padding := m.width - lipgloss.Width(left) - lipgloss.Width(stats)
	if padding < 1 {
		padding = 1
	}
	b.WriteString(left + strings.Repeat(" ", padding) + stats)
	b.WriteString("\n")

	// Content based on current tab
	b.WriteString(m.renderDashboardContent())

	// Footer
	help := "[enter]drill [tab]switch [n]ew [L]ogs [e]nvim [s]hell [q]quit"
	if m.tab == TabNotes {
		help = "[n]ew [x/space]toggle [d]elete [q]quit"
	} else if m.tab == TabChangelog {
		help = "[tab]switch [d]elete [q]quit"
	}
	update := fmt.Sprintf("%s ago", formatDuration(time.Since(m.lastUpdate)))
	footerPad := m.width - lipgloss.Width(help) - lipgloss.Width(update) - 2
	if footerPad < 1 {
		footerPad = 1
	}
	b.WriteString(tui.StyleHelp.Render(help) + strings.Repeat(" ", footerPad) + tui.StyleMuted.Render(update))

	return b.String()
}

func (m Model) renderDashboardContent() string {
	switch m.tab {
	case TabWorktrees:
		return m.renderAllWorktrees()
	case TabJobs:
		return m.renderAllJobs()
	case TabAgents:
		return m.renderAllAgents()
	case TabNotes:
		return m.renderNotes()
	case TabChangelog:
		return m.renderChangelog()
	}
	return ""
}

func (m Model) renderAllWorktrees() string {
	var b strings.Builder

	contentHeight := m.height - 4
	if contentHeight < 1 {
		contentHeight = 1
	}

	header := fmt.Sprintf(" %-15s %-25s %-18s %s  %s", "PROJECT", "PATH", "BRANCH", "ST", "GIT")
	b.WriteString(tui.StyleHeader.Render(header))
	b.WriteString("\n")

	if len(m.worktrees) == 0 {
		b.WriteString(tui.StyleMuted.Render(" No worktrees found. Configure base_dirs in ~/.config/athena/config.yaml"))
		b.WriteString("\n")
		for i := 1; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	for i := 0; i < contentHeight-1; i++ {
		if i < len(m.worktrees) {
			wt := m.worktrees[i]
			row := m.renderGlobalWorktreeRow(wt, i == m.selected)
			b.WriteString(row)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderGlobalWorktreeRow(wt *control.WorktreeInfo, selected bool) string {
	icon := tui.StyleMuted.Render("·")
	if wt.AgentID != "" {
		for _, a := range m.agents {
			if a.ID == wt.AgentID {
				icon = tui.StatusStyle(a.Status).Render(tui.AgentIcons[a.Status])
				break
			}
		}
	}

	path := filepath.Base(wt.Path)
	if wt.IsMain {
		path = path + " (main)"
	}

	row := fmt.Sprintf(" %-15s %-25s %-18s %s  %s",
		truncate(wt.Project, 15),
		truncate(path, 25),
		truncate(wt.Branch, 18),
		icon,
		wt.Status,
	)

	if selected {
		return tui.StyleSelected.Render(row)
	}
	return row
}

func (m Model) renderAllJobs() string {
	var b strings.Builder

	contentHeight := m.height - 4
	if contentHeight < 1 {
		contentHeight = 1
	}

	header := fmt.Sprintf(" %s  %-12s %-40s %-10s %s", "#", "PROJECT", "TASK", "STATUS", "AGENT")
	b.WriteString(tui.StyleHeader.Render(header))
	b.WriteString("\n")

	if len(m.jobs) == 0 {
		b.WriteString(tui.StyleMuted.Render(" No jobs. Enter a project and press [n] to create one."))
		b.WriteString("\n")
		for i := 1; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	for i := 0; i < contentHeight-1; i++ {
		if i < len(m.jobs) {
			job := m.jobs[i]
			row := m.renderGlobalJobRow(job, i, i == m.selected)
			b.WriteString(row)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderGlobalJobRow(job *control.JobInfo, idx int, selected bool) string {
	agentIcon := tui.StyleMuted.Render("·")
	if job.AgentID != "" {
		for _, a := range m.agents {
			if a.ID == job.AgentID {
				agentIcon = tui.StatusStyle(a.Status).Render(tui.AgentIcons[a.Status])
				break
			}
		}
	}

	row := fmt.Sprintf(" %2d  %-12s %-40s %-10s %s",
		idx+1,
		truncate(job.Project, 12),
		truncate(job.NormalizedInput, 40),
		job.Status,
		agentIcon,
	)

	if selected {
		return tui.StyleSelected.Render(row)
	}
	return row
}

func (m Model) renderAllAgents() string {
	var b strings.Builder

	contentHeight := m.height - 4
	if contentHeight < 1 {
		contentHeight = 1
	}

	header := fmt.Sprintf(" %s  %-12s %-10s %-22s %-12s %s", "ST", "PROJECT", "TYPE", "WORKTREE", "STATUS", "AGE")
	b.WriteString(tui.StyleHeader.Render(header))
	b.WriteString("\n")

	if len(m.agents) == 0 {
		b.WriteString(tui.StyleMuted.Render(" No agents running."))
		b.WriteString("\n")
		for i := 1; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	for i := 0; i < contentHeight-1; i++ {
		if i < len(m.agents) {
			agent := m.agents[i]
			row := m.renderGlobalAgentRow(agent, i == m.selected)
			b.WriteString(row)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderGlobalAgentRow(agent *control.AgentInfo, selected bool) string {
	icon := tui.StatusStyle(agent.Status).Render(tui.AgentIcons[agent.Status])
	wtName := filepath.Base(agent.WorktreePath)
	age := formatDuration(time.Since(parseCreatedAt(agent.CreatedAt)))

	row := fmt.Sprintf(" %s  %-12s %-10s %-22s %-12s %s",
		icon,
		truncate(agent.Project, 12),
		truncate(agent.Archetype, 10),
		truncate(wtName, 22),
		agent.Status,
		age,
	)

	if selected {
		return tui.StyleSelected.Render(row)
	}
	return row
}

func (m Model) renderProjectDetail() string {
	var b strings.Builder

	// Header with project name and tabs
	b.WriteString(m.renderDetailHeader())
	b.WriteString("\n")

	// Content
	if m.inputMode {
		b.WriteString(m.renderInput())
	} else {
		b.WriteString(m.renderDetailContent())
	}

	// Footer
	b.WriteString(m.renderDetailFooter())

	return b.String()
}

func (m Model) renderDetailHeader() string {
	// Icon and project name
	icon := tui.StyleAccent.Render("▐▛▜▌ ")
	proj := icon + tui.StyleAccent.Bold(true).Render(m.selectedProject)

	// Tabs (keys not shown but still work via 1-6)
	tabs := []struct {
		name string
		tab  Tab
	}{
		{"worktrees", TabWorktrees},
		{"jobs", TabJobs},
		{"agents", TabAgents},
		{"plans", TabPlans},
		{"notes", TabNotes},
		{"changelog", TabChangelog},
	}

	var tabParts []string
	for _, t := range tabs {
		if t.tab == m.tab {
			tabParts = append(tabParts, tui.StyleAccent.Render(t.name))
		} else {
			tabParts = append(tabParts, tui.StyleMuted.Render(t.name))
		}
	}
	tabStr := strings.Join(tabParts, "  ") // More spacing between tabs

	// Stats
	agents := m.projectAgents()
	activeCount := 0
	for _, a := range agents {
		if a.Status == "running" || a.Status == "planning" || a.Status == "executing" {
			activeCount++
		}
	}
	stats := tui.StyleMuted.Render(fmt.Sprintf("%d/%d", activeCount, len(agents)))

	// Layout
	sep := tui.StyleMuted.Render(" │ ")
	left := proj + sep + tabStr
	padding := m.width - lipgloss.Width(left) - lipgloss.Width(stats)
	if padding < 1 {
		padding = 1
	}

	return left + strings.Repeat(" ", padding) + stats
}

func (m Model) renderDetailContent() string {
	switch m.tab {
	case TabWorktrees:
		return m.renderWorktrees()
	case TabJobs:
		return m.renderJobs()
	case TabAgents:
		return m.renderAgents()
	case TabPlans:
		return m.renderPlans()
	case TabNotes:
		return m.renderNotes()
	case TabChangelog:
		return m.renderChangelog()
	}
	return ""
}

func (m Model) renderWorktrees() string {
	var b strings.Builder
	wts := m.projectWorktrees()

	contentHeight := m.height - 4
	if contentHeight < 1 {
		contentHeight = 1
	}

	header := fmt.Sprintf(" %-30s %-20s %s  %s", "PATH", "BRANCH", "ST", "GIT")
	b.WriteString(tui.StyleHeader.Render(header))
	b.WriteString("\n")

	if len(wts) == 0 {
		b.WriteString(tui.StyleMuted.Render(" No worktrees for this project."))
		b.WriteString("\n")
		for i := 1; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	for i := 0; i < contentHeight-1; i++ {
		if i < len(wts) {
			wt := wts[i]
			row := m.renderWorktreeRow(wt, i == m.selected)
			b.WriteString(row)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderWorktreeRow(wt *control.WorktreeInfo, selected bool) string {
	icon := tui.StyleMuted.Render("·")
	if wt.AgentID != "" {
		for _, a := range m.agents {
			if a.ID == wt.AgentID {
				icon = tui.StatusStyle(a.Status).Render(tui.AgentIcons[a.Status])
				break
			}
		}
	}

	path := filepath.Base(wt.Path)
	if wt.IsMain {
		path = path + " (main)"
	}

	row := fmt.Sprintf(" %-30s %-20s %s  %s",
		truncate(path, 30),
		truncate(wt.Branch, 20),
		icon,
		wt.Status,
	)

	if selected {
		return tui.StyleSelected.Render(row)
	}
	return row
}

func (m Model) renderJobs() string {
	var b strings.Builder
	jobs := m.projectJobs()

	contentHeight := m.height - 4
	if contentHeight < 1 {
		contentHeight = 1
	}

	header := fmt.Sprintf(" %s  %-45s %-10s %s", "#", "TASK", "STATUS", "AGENT")
	b.WriteString(tui.StyleHeader.Render(header))
	b.WriteString("\n")

	if len(jobs) == 0 {
		b.WriteString(tui.StyleMuted.Render(" No jobs. Press [n] to create one."))
		b.WriteString("\n")
		for i := 1; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	for i := 0; i < contentHeight-1; i++ {
		if i < len(jobs) {
			job := jobs[i]
			row := m.renderJobRow(job, i, i == m.selected)
			b.WriteString(row)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderJobRow(job *control.JobInfo, idx int, selected bool) string {
	agentIcon := tui.StyleMuted.Render("·")
	if job.AgentID != "" {
		for _, a := range m.agents {
			if a.ID == job.AgentID {
				agentIcon = tui.StatusStyle(a.Status).Render(tui.AgentIcons[a.Status])
				break
			}
		}
	}

	// Colorize job status
	status := tui.StatusStyle(job.Status).Render(fmt.Sprintf("%-10s", job.Status))

	row := fmt.Sprintf(" %2d  %-45s %s %s",
		idx+1,
		truncate(job.NormalizedInput, 45),
		status,
		agentIcon,
	)

	if selected {
		return tui.StyleSelected.Render(row)
	}
	return row
}

func (m Model) renderAgents() string {
	var b strings.Builder
	agents := m.projectAgents()

	contentHeight := m.height - 4
	if contentHeight < 1 {
		contentHeight = 1
	}

	header := fmt.Sprintf(" %s  %-10s %-25s %-12s %s", "ST", "TYPE", "WORKTREE", "STATUS", "AGE")
	b.WriteString(tui.StyleHeader.Render(header))
	b.WriteString("\n")

	if len(agents) == 0 {
		b.WriteString(tui.StyleMuted.Render(" No agents running."))
		b.WriteString("\n")
		for i := 1; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	for i := 0; i < contentHeight-1; i++ {
		if i < len(agents) {
			agent := agents[i]
			row := m.renderAgentRow(agent, i == m.selected)
			b.WriteString(row)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderAgentRow(agent *control.AgentInfo, selected bool) string {
	icon := tui.StatusStyle(agent.Status).Render(tui.AgentIcons[agent.Status])
	wtName := filepath.Base(agent.WorktreePath)

	age := formatDuration(time.Since(parseCreatedAt(agent.CreatedAt)))

	// Colorize status
	status := tui.StatusStyle(agent.Status).Render(fmt.Sprintf("%-12s", agent.Status))

	row := fmt.Sprintf(" %s  %-10s %-25s %s %s",
		icon,
		truncate(agent.Archetype, 10),
		truncate(wtName, 25),
		status,
		age,
	)

	if selected {
		return tui.StyleSelected.Render(row)
	}
	return row
}

func (m Model) renderPlans() string {
	var b strings.Builder
	plans := m.projectPlans()

	contentHeight := m.height - 4
	if contentHeight < 1 {
		contentHeight = 1
	}

	header := fmt.Sprintf(" %-30s %-20s %s", "PLAN", "AGENT", "STATUS")
	b.WriteString(tui.StyleHeader.Render(header))
	b.WriteString("\n")

	if len(plans) == 0 {
		b.WriteString(tui.StyleMuted.Render(" No plans. Agents in planning mode will appear here."))
		b.WriteString("\n")
		for i := 1; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	for i := 0; i < contentHeight-1; i++ {
		if i < len(plans) {
			plan := plans[i]
			row := m.renderPlanRow(plan, i == m.selected)
			b.WriteString(row)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderPlanRow(plan Plan, selected bool) string {
	row := fmt.Sprintf(" %-30s %-20s %s",
		truncate(plan.Title, 30),
		truncate(plan.AgentID[:8]+"...", 20),
		"ready",
	)

	if selected {
		return tui.StyleSelected.Render(row)
	}
	return row
}

func (m Model) renderNotes() string {
	var b strings.Builder

	contentHeight := m.height - 4
	if contentHeight < 1 {
		contentHeight = 1
	}

	header := fmt.Sprintf(" %s  %-60s", "✓", "NOTE")
	b.WriteString(tui.StyleHeader.Render(header))
	b.WriteString("\n")

	if len(m.notes) == 0 {
		b.WriteString(tui.StyleMuted.Render(" No notes yet. Quick ideas and todos go here."))
		b.WriteString("\n")
		for i := 1; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	for i := 0; i < contentHeight-1; i++ {
		if i < len(m.notes) {
			note := m.notes[i]
			row := m.renderNoteRow(note, i == m.selected)
			b.WriteString(row)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderNoteRow(note *control.NoteInfo, selected bool) string {
	check := "○"
	if note.Done {
		check = "●"
	}

	row := fmt.Sprintf(" %s  %s", check, truncate(note.Content, 60))

	if selected {
		return tui.StyleSelected.Render(row)
	}
	if note.Done {
		return tui.StyleMuted.Render(row)
	}
	return row
}

func (m Model) renderChangelog() string {
	var b strings.Builder

	contentHeight := m.height - 4
	if contentHeight < 1 {
		contentHeight = 1
	}

	header := fmt.Sprintf(" %-12s %-40s %-15s %s", "CATEGORY", "TITLE", "PROJECT", "DATE")
	b.WriteString(tui.StyleHeader.Render(header))
	b.WriteString("\n")

	// Use project-filtered changelog if in project level, otherwise all
	entries := m.changelog
	if m.level == LevelProject {
		entries = m.projectChangelog()
	}

	if len(entries) == 0 {
		b.WriteString(tui.StyleMuted.Render(" No changelog entries yet. Completed work will be tracked here."))
		b.WriteString("\n")
		for i := 1; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	for i := 0; i < contentHeight-1; i++ {
		if i < len(entries) {
			entry := entries[i]
			row := m.renderChangelogRow(entry, i == m.selected)
			b.WriteString(row)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderChangelogRow(entry *control.ChangelogInfo, selected bool) string {
	// Category icon
	var catIcon string
	switch entry.Category {
	case "feature":
		catIcon = "✨"
	case "fix":
		catIcon = "🔧"
	case "refactor":
		catIcon = "♻️"
	case "docs":
		catIcon = "📚"
	default:
		catIcon = "•"
	}

	// Parse and format date
	date := ""
	if t, err := time.Parse(time.RFC3339, entry.CreatedAt); err == nil {
		date = t.Format("Jan 02")
	}

	row := fmt.Sprintf(" %s %-10s %-40s %-15s %s",
		catIcon,
		entry.Category,
		truncate(entry.Title, 40),
		truncate(entry.Project, 15),
		date)

	if selected {
		return tui.StyleSelected.Render(row)
	}
	return row
}

func (m Model) renderJobDetail() string {
	var b strings.Builder
	job := m.detailJob

	// Header
	icon := tui.StyleAccent.Render("▐▛▜▌ ")
	typeLabel := "Job"
	if job.Type == "question" {
		typeLabel = "Question"
	}
	b.WriteString(icon + tui.StyleAccent.Bold(true).Render(typeLabel))
	b.WriteString(tui.StyleMuted.Render(" │ " + job.Status))
	b.WriteString("\n\n")

	// Task/Question
	b.WriteString(tui.StyleMuted.Render("  Task: "))
	b.WriteString(job.NormalizedInput)
	b.WriteString("\n\n")

	// Answer (for question jobs)
	if job.Type == "question" {
		b.WriteString(tui.StyleMuted.Render("  Answer:\n"))
		if job.Answer != "" {
			// Word wrap the answer
			lines := wrapText(job.Answer, m.width-4)
			for _, line := range lines {
				b.WriteString("  " + line + "\n")
			}
		} else if job.Status == "pending" || job.Status == "executing" {
			b.WriteString(tui.StyleMuted.Render("  Processing...\n"))
		} else {
			b.WriteString(tui.StyleMuted.Render("  No answer yet.\n"))
		}
	} else {
		// For feature jobs show more info
		if job.AgentID != "" {
			b.WriteString(tui.StyleMuted.Render("  Agent: "))
			b.WriteString(job.AgentID[:8] + "...")
			b.WriteString("\n")
		}
		if job.WorktreePath != "" {
			b.WriteString(tui.StyleMuted.Render("  Worktree: "))
			b.WriteString(job.WorktreePath)
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(tui.StyleHelp.Render("  Press Esc or Enter to close"))
	b.WriteString("\n")

	return b.String()
}

func (m Model) renderAgentDetail() string {
	var b strings.Builder
	agent := m.detailAgent

	// Header with icon and status
	icon := tui.StatusStyle(agent.Status).Render(tui.AgentIcons[agent.Status])
	b.WriteString(icon + " ")
	b.WriteString(tui.StyleAccent.Bold(true).Render("Agent"))
	b.WriteString(tui.StyleMuted.Render(" │ "))
	b.WriteString(tui.StatusStyle(agent.Status).Render(agent.Status))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width-2))
	b.WriteString("\n\n")

	// Key info
	b.WriteString(tui.StyleMuted.Render("  ID:        "))
	b.WriteString(agent.ID)
	b.WriteString("\n")

	b.WriteString(tui.StyleMuted.Render("  Project:   "))
	b.WriteString(agent.ProjectName)
	b.WriteString("\n")

	b.WriteString(tui.StyleMuted.Render("  Archetype: "))
	b.WriteString(agent.Archetype)
	b.WriteString("\n")

	b.WriteString(tui.StyleMuted.Render("  Worktree:  "))
	b.WriteString(agent.WorktreePath)
	b.WriteString("\n")

	if agent.LinearIssueID != "" {
		b.WriteString(tui.StyleMuted.Render("  Ticket:    "))
		b.WriteString(tui.StyleAccent.Render(agent.LinearIssueID))
		b.WriteString("\n")
	}

	age := formatDuration(time.Since(parseCreatedAt(agent.CreatedAt)))
	b.WriteString(tui.StyleMuted.Render("  Age:       "))
	b.WriteString(age)
	b.WriteString("\n")

	b.WriteString(tui.StyleMuted.Render("  Restarts:  "))
	b.WriteString(fmt.Sprintf("%d", agent.RestartCount))
	b.WriteString("\n")

	// Prompt/Task
	if agent.Prompt != "" {
		b.WriteString("\n")
		b.WriteString(tui.StyleMuted.Render("  Task:\n"))
		lines := wrapText(agent.Prompt, m.width-6)
		for _, line := range lines {
			b.WriteString("    " + line + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(tui.StyleHelp.Render("  [L]ogs [a]ttach [e]nvim [s]hell [x]kill │ Esc to close"))
	b.WriteString("\n")

	return b.String()
}

func (m Model) renderLogs() string {
	var b strings.Builder

	// Header
	icon := tui.StyleAccent.Render("▐▛▜▌ ")
	b.WriteString(icon + tui.StyleAccent.Bold(true).Render("Agent Logs"))
	b.WriteString(tui.StyleMuted.Render(" │ " + m.logsAgentID[:8] + "..."))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width-2))
	b.WriteString("\n\n")

	if len(m.logs) == 0 {
		b.WriteString(tui.StyleMuted.Render("  No events recorded."))
		b.WriteString("\n")
	} else {
		// Show logs in chronological order (reverse since they come DESC)
		maxLines := m.height - 8
		if maxLines < 5 {
			maxLines = 5
		}
		start := 0
		if len(m.logs) > maxLines {
			start = len(m.logs) - maxLines
		}

		for i := len(m.logs) - 1; i >= start; i-- {
			e := m.logs[i]
			ts, _ := time.Parse(time.RFC3339, e.Timestamp)
			age := time.Since(ts)

			// Event type badge
			typeStyle := tui.StyleMuted
			switch e.EventType {
			case "spawned":
				typeStyle = tui.StatusStyle("running")
			case "output", "text":
				typeStyle = tui.StyleAccent
			case "error", "crashed":
				typeStyle = tui.StatusStyle("crashed")
			case "completed":
				typeStyle = tui.StatusStyle("completed")
			case "tool_use":
				typeStyle = tui.StatusStyle("planning")
			}

			b.WriteString("  ")
			b.WriteString(tui.StyleMuted.Render(formatDuration(age) + " ago"))
			b.WriteString("  ")
			b.WriteString(typeStyle.Render(fmt.Sprintf("%-10s", e.EventType)))
			b.WriteString("  ")

			// Truncate payload for display
			payload := e.Payload
			maxPayload := m.width - 30
			if maxPayload < 20 {
				maxPayload = 20
			}
			if len(payload) > maxPayload {
				payload = payload[:maxPayload-3] + "..."
			}
			b.WriteString(payload)
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(tui.StyleHelp.Render("  [r]efresh  [q/Esc] close"))
	b.WriteString("\n")

	return b.String()
}

func wrapText(text string, width int) []string {
	if width <= 0 {
		width = 80
	}
	var lines []string
	words := strings.Fields(text)
	var line string
	for _, word := range words {
		if len(line)+len(word)+1 > width {
			if line != "" {
				lines = append(lines, line)
			}
			line = word
		} else {
			if line != "" {
				line += " "
			}
			line += word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func (m Model) renderInput() string {
	var b strings.Builder

	// Minimal padding - just 2 lines
	b.WriteString("\n\n")

	title := "New Job"
	hint := "[?] for question"
	if m.noteMode {
		title = "New Note"
		hint = ""
	} else if m.questionMode {
		title = "Question"
		hint = "[n] for job"
	}

	b.WriteString(tui.StyleTitle.Render("  " + title))
	if hint != "" {
		b.WriteString(tui.StyleMuted.Render("  " + hint))
	}
	b.WriteString("\n\n")
	b.WriteString("  ")
	b.WriteString(m.textInput.View())
	b.WriteString("\n\n")
	b.WriteString(tui.StyleMuted.Render("  Enter to submit · Esc to cancel"))
	b.WriteString("\n")

	return b.String()
}

func (m Model) renderDetailFooter() string {
	help := "[n]ew [?]ask [a]ttach [L]ogs [v]iew [e]nvim [s]hell [x]kill [q]back"
	if m.tab == TabNotes {
		help = "[n]ew [x/space]toggle [d]elete [q]back"
	}
	update := fmt.Sprintf("%s ago", formatDuration(time.Since(m.lastUpdate)))

	padding := m.width - lipgloss.Width(help) - lipgloss.Width(update) - 2
	if padding < 1 {
		padding = 1
	}

	return tui.StyleHelp.Render(help) + strings.Repeat(" ", padding) + tui.StyleMuted.Render(update)
}

// Actions

func (m *Model) doAttach() {
	var wts []*control.WorktreeInfo
	var agents []*control.AgentInfo

	if m.level == LevelDashboard {
		wts = m.worktrees
		agents = m.agents
	} else {
		wts = m.projectWorktrees()
		agents = m.projectAgents()
	}

	if m.tab == TabWorktrees {
		if m.selected < len(wts) {
			wt := wts[m.selected]
			if wt.AgentID != "" {
				m.term.AttachToAgent(wt.Path, wt.AgentID)
			}
		}
	} else if m.tab == TabAgents {
		if m.selected < len(agents) {
			a := agents[m.selected]
			m.term.AttachToAgent(a.WorktreePath, a.ID)
		}
	}
}

func (m *Model) doOpenNvim() {
	var wts []*control.WorktreeInfo
	var agents []*control.AgentInfo

	if m.level == LevelDashboard {
		wts = m.worktrees
		agents = m.agents
	} else {
		wts = m.projectWorktrees()
		agents = m.projectAgents()
	}

	if m.tab == TabWorktrees {
		if m.selected < len(wts) {
			m.term.OpenNvim(wts[m.selected].Path, true)
		}
	} else if m.tab == TabAgents {
		if m.selected < len(agents) {
			m.term.OpenNvim(agents[m.selected].WorktreePath, true)
		}
	}
}

func (m *Model) doView() {
	var wts []*control.WorktreeInfo
	var agents []*control.AgentInfo

	if m.level == LevelDashboard {
		wts = m.worktrees
		agents = m.agents
	} else {
		wts = m.projectWorktrees()
		agents = m.projectAgents()
	}

	var agentID string
	if m.tab == TabWorktrees {
		if m.selected < len(wts) {
			agentID = wts[m.selected].AgentID
		}
	} else if m.tab == TabAgents {
		if m.selected < len(agents) {
			agentID = agents[m.selected].ID
		}
	}

	if agentID != "" {
		m.term.OpenTab("", "athena", "view", agentID)
	}
}

func (m *Model) doShell() {
	var wts []*control.WorktreeInfo

	if m.level == LevelDashboard {
		wts = m.worktrees
	} else {
		wts = m.projectWorktrees()
	}

	if m.tab == TabWorktrees {
		if m.selected < len(wts) {
			m.term.OpenShell(wts[m.selected].Path)
		}
	}
}

func (m Model) doLogs() tea.Cmd {
	var wts []*control.WorktreeInfo
	var agents []*control.AgentInfo

	if m.level == LevelDashboard {
		wts = m.worktrees
		agents = m.agents
	} else {
		wts = m.projectWorktrees()
		agents = m.projectAgents()
	}

	var agentID string
	if m.tab == TabWorktrees {
		if m.selected < len(wts) {
			agentID = wts[m.selected].AgentID
		}
	} else if m.tab == TabAgents {
		if m.selected < len(agents) {
			agentID = agents[m.selected].ID
		}
	}

	if agentID != "" {
		return m.fetchLogs(agentID)
	}
	return nil
}

func (m Model) doKill() tea.Cmd {
	var wts []*control.WorktreeInfo
	var agents []*control.AgentInfo

	if m.level == LevelDashboard {
		wts = m.worktrees
		agents = m.agents
	} else {
		wts = m.projectWorktrees()
		agents = m.projectAgents()
	}

	var agentID string
	if m.tab == TabWorktrees {
		if m.selected < len(wts) {
			agentID = wts[m.selected].AgentID
		}
	} else if m.tab == TabAgents {
		if m.selected < len(agents) {
			agentID = agents[m.selected].ID
		}
	}

	if agentID != "" {
		return m.killAgent(agentID)
	}
	return nil
}

func (m Model) doNormalize() tea.Cmd {
	return func() tea.Msg {
		_, err := m.client.Normalize()
		if err != nil {
			return errMsg(err)
		}
		return dataUpdateMsg{}
	}
}

// Commands

func (m Model) tick() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) fetchData() tea.Msg {
	if m.client == nil {
		return errMsg(fmt.Errorf("not connected"))
	}

	wts, _ := m.client.ListWorktrees()
	agents, _ := m.client.ListAgents()
	jobs, _ := m.client.ListJobs()
	notes, _ := m.client.ListNotes()
	changelog, _ := m.client.ListChangelog("", 100)

	return fetchDataResultMsg{
		worktrees: wts,
		agents:    agents,
		jobs:      jobs,
		notes:     notes,
		changelog: changelog,
	}
}

func (m Model) listenForEvents() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		event := <-m.client.Events()
		return eventMsg(event)
	}
}

func (m Model) createJob(input string, isQuestion bool) tea.Cmd {
	return func() tea.Msg {
		jobType := "feature"
		if isQuestion {
			jobType = "question"
		}
		_, err := m.client.CreateJob(control.CreateJobRequest{
			Input:   input,
			Project: m.selectedProject,
			Type:    jobType,
		})
		if err != nil {
			return errMsg(err)
		}
		return dataUpdateMsg{}
	}
}

func (m Model) killAgent(id string) tea.Cmd {
	return func() tea.Msg {
		if err := m.client.KillAgent(id); err != nil {
			return errMsg(err)
		}
		return dataUpdateMsg{}
	}
}

func (m Model) createNote(content string) tea.Cmd {
	return func() tea.Msg {
		_, err := m.client.CreateNote(control.CreateNoteRequest{
			Content: content,
		})
		if err != nil {
			return errMsg(err)
		}
		return dataUpdateMsg{}
	}
}

func (m Model) toggleNote(id string, done bool) tea.Cmd {
	return func() tea.Msg {
		err := m.client.UpdateNote(control.UpdateNoteRequest{
			ID:   id,
			Done: done,
		})
		if err != nil {
			return errMsg(err)
		}
		return dataUpdateMsg{}
	}
}

func (m Model) deleteNote(id string) tea.Cmd {
	return func() tea.Msg {
		if err := m.client.DeleteNote(id); err != nil {
			return errMsg(err)
		}
		return dataUpdateMsg{}
	}
}

func (m Model) deleteChangelog(id string) tea.Cmd {
	return func() tea.Msg {
		if err := m.client.DeleteChangelog(id); err != nil {
			return errMsg(err)
		}
		return dataUpdateMsg{}
	}
}

func (m Model) fetchLogs(agentID string) tea.Cmd {
	return func() tea.Msg {
		logs, err := m.client.GetAgentLogs(agentID, 100)
		if err != nil {
			return errMsg(err)
		}
		return logsResultMsg{agentID: agentID, logs: logs}
	}
}

// Helpers

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

// parseCreatedAt parses an RFC3339 timestamp string into a time.Time.
// Returns zero time if parsing fails.
func parseCreatedAt(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
