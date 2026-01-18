// Package dashboard provides the main TUI dashboard view.
package dashboard

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/terminal"
	"github.com/drewfead/athena/internal/tui"
	"github.com/drewfead/athena/internal/tui/layout"
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
	TabProjects  Tab = iota // Dashboard: overview of all projects
	TabWorktrees            // All worktrees (dashboard) or project worktrees (drill-in)
	TabJobs                 // All active AI invocations (dashboard) or project tasks (drill-in)
	TabAgents               // Long-running agents (drill-in only)
	TabTasks                // Short-lived tasks (drill-in only)
	TabNotes                // Project-specific notes (drill-in only)
	TabQuestions            // Quick Q&A - project-less (dashboard only)
)

// Dashboard level tabs (global view)
var dashboardTabs = []Tab{TabProjects, TabWorktrees, TabJobs, TabQuestions}

// Project level tabs (drill-in view)
var projectTabs = []Tab{TabWorktrees, TabAgents, TabTasks, TabNotes}

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
	level           Level
	selectedProject string
	tab             Tab
	selected        int

	// Scrolling
	scrollOffset map[Tab]int

	// UI state
	width        int
	height       int
	inputMode    bool
	questionMode bool // true = question, false = job
	noteMode     bool // true = adding note
	detailMode   bool // showing detail view
	detailJob    *control.JobInfo
	detailAgent  *control.AgentInfo
	logsMode     bool // showing agent logs
	logsAgentID  string
	logs         []*control.AgentEventInfo
	logsScroll   int  // scroll offset for logs viewport
	logsFollow   bool // auto-scroll to bottom
	textInput    textarea.Model
	spinner      spinner.Model
	lastUpdate   time.Time
	err          error

	// Worktree creation wizard
	worktreeMode       bool   // true = creating worktree
	worktreeStep       int    // 0=ticket, 1=description, 2=project
	worktreeTicketID   string // ticket ID from step 0
	worktreeDesc       string // description from step 1
	worktreeProjectIdx int    // selected project index in step 2

	// Note promotion wizard
	promoteMode       bool   // true = promoting note to feature
	promoteNoteID     string // ID of note being promoted
	promoteNoteText   string // content of note being promoted
	promoteProjectIdx int    // selected project index

	// Plan viewer mode
	planMode          bool   // true = viewing plan
	planContent       string // raw markdown content
	planRendered      string // glamour-rendered output
	planStatus        string // pending | draft | approved | executing | completed
	planPlannerStatus string // Status of planner agent (for visibility when pending)
	planScroll        int    // scroll offset
	planAgentID       string // planner agent ID for parent link
	planWorktreePath  string // worktree path for the plan

	// Status message feedback
	statusMsg     string
	statusMsgTime time.Time

	// Layout tables (cached, updated on resize)
	worktreeTable *layout.Table
	jobTable      *layout.Table
	agentTable    *layout.Table

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
	planResultMsg struct {
		worktreePath  string
		content       string
		status        string
		agentID       string
		plannerStatus string // Status of planner agent (for pending plans)
	}
	publishResultMsg struct {
		path   string
		prURL  string
		branch string
	}
	mergeResultMsg struct {
		path         string
		hasConflicts bool
		agentSpawned bool
		message      string
	}
	cleanupResultMsg struct {
		path string
	}
	clearStatusMsg struct{}
	clearErrMsg    struct{} // Auto-clears error after timeout
)

// New creates a new dashboard model
func New(client *control.Client) Model {
	ti := textarea.New()
	ti.Placeholder = "Type here..."
	ti.CharLimit = 2000
	ti.SetWidth(80) // Will be updated on resize
	ti.SetHeight(3) // Start with 3 lines
	ti.ShowLineNumbers = false
	ti.FocusedStyle.CursorLine = lipgloss.NewStyle() // No highlight on current line
	ti.FocusedStyle.Base = lipgloss.NewStyle()       // No border/background
	ti.BlurredStyle.Base = lipgloss.NewStyle()       // No border/background when blurred

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(tui.ColorAccent)

	// Create responsive tables with generous max widths for wide terminals
	worktreeTable := layout.NewTable([]layout.Column{
		{Header: "PROJECT", MinWidth: 12, MaxWidth: 30, Flex: 1},
		{Header: "PATH", MinWidth: 20, MaxWidth: 60, Flex: 3},
		{Header: "BRANCH", MinWidth: 15, MaxWidth: 40, Flex: 2},
		{Header: "ST", MinWidth: 2, MaxWidth: 2, Flex: 0},
		{Header: "GIT", MinWidth: 12, MaxWidth: 40, Flex: 1}, // Starship-style [!?+*→branch]
	})

	jobTable := layout.NewTable([]layout.Column{
		{Header: "#", MinWidth: 3, MaxWidth: 5, Flex: 0},
		{Header: "PROJECT", MinWidth: 10, MaxWidth: 25, Flex: 1},
		{Header: "TASK", MinWidth: 30, MaxWidth: 0, Flex: 4}, // 0 = unlimited
		{Header: "STATUS", MinWidth: 10, MaxWidth: 14, Flex: 0},
		{Header: "AGENT", MinWidth: 2, MaxWidth: 4, Flex: 0},
	})

	agentTable := layout.NewTable([]layout.Column{
		{Header: "ST", MinWidth: 2, MaxWidth: 2, Flex: 0},
		{Header: "PROJECT", MinWidth: 10, MaxWidth: 25, Flex: 1},
		{Header: "TYPE", MinWidth: 8, MaxWidth: 14, Flex: 0},
		{Header: "WORKTREE", MinWidth: 18, MaxWidth: 50, Flex: 3},
		{Header: "STATUS", MinWidth: 10, MaxWidth: 14, Flex: 0},
		{Header: "AGE", MinWidth: 5, MaxWidth: 10, Flex: 0},
	})

	return Model{
		client:        client,
		level:         LevelDashboard,
		tab:           TabWorktrees,
		spinner:       sp,
		textInput:     ti,
		term:          terminal.Detect(),
		scrollOffset:  make(map[Tab]int),
		worktreeTable: worktreeTable,
		jobTable:      jobTable,
		agentTable:    agentTable,
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
		if m.promoteMode {
			return m.handlePromoteNote(msg)
		}
		if m.worktreeMode {
			return m.handleWorktreeWizard(msg)
		}
		if m.inputMode {
			return m.handleInputMode(msg)
		}
		if m.planMode {
			return m.handlePlanMode(msg)
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

		// Update text input width dynamically
		inputWidth := msg.Width - 10
		if inputWidth < 30 {
			inputWidth = 30
		}
		if inputWidth > 100 {
			inputWidth = 100
		}
		m.textInput.SetWidth(inputWidth)

		// Update table widths
		m.worktreeTable.SetWidth(msg.Width)
		m.jobTable.SetWidth(msg.Width)
		m.agentTable.SetWidth(msg.Width)

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
		// Also set as status message for more visibility
		m.statusMsg = "✗ " + msg.Error()
		m.statusMsgTime = time.Now()
		// Auto-clear error after 5 seconds
		return m, tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
			return clearErrMsg{}
		})

	case clearErrMsg:
		m.err = nil

	case dataUpdateMsg:
		// Data was modified (create/update/delete), refresh
		cmds = append(cmds, m.fetchData)

	case eventMsg:
		cmds = append(cmds, m.fetchData, m.listenForEvents())

	case logsResultMsg:
		m.logsAgentID = msg.agentID
		m.logs = msg.logs
		m.logsMode = true
		m.logsFollow = true
		// Start at bottom (most recent) if following
		m.logsScroll = max(0, len(m.logs)-m.logsViewportHeight())

	case planResultMsg:
		m.planWorktreePath = msg.worktreePath
		m.planContent = msg.content
		m.planStatus = msg.status
		m.planAgentID = msg.agentID
		m.planPlannerStatus = msg.plannerStatus
		m.planMode = true
		m.planScroll = 0
		// Render markdown with glamour (or show pending message)
		if msg.status == "pending" {
			m.planRendered = m.renderPendingPlan(msg.plannerStatus)
		} else {
			m.planRendered = m.renderMarkdown(msg.content)
		}

	case publishResultMsg:
		m.statusMsg = fmt.Sprintf("PR created: %s", msg.prURL)
		m.statusMsgTime = time.Now()
		cmds = append(cmds, m.fetchData, tea.Tick(5*time.Second, func(time.Time) tea.Msg {
			return clearStatusMsg{}
		}))

	case mergeResultMsg:
		if msg.hasConflicts {
			if msg.agentSpawned {
				m.statusMsg = "⚡ Merge conflict detected - resolver agent spawned"
			} else {
				m.statusMsg = "⚠ Merge conflict detected"
			}
		} else {
			m.statusMsg = "✓ Branch merged to main"
		}
		m.statusMsgTime = time.Now()
		cmds = append(cmds, m.fetchData, tea.Tick(5*time.Second, func(time.Time) tea.Msg {
			return clearStatusMsg{}
		}))

	case cleanupResultMsg:
		m.statusMsg = fmt.Sprintf("Worktree cleaned up: %s", filepath.Base(msg.path))
		m.statusMsgTime = time.Now()
		cmds = append(cmds, m.fetchData, tea.Tick(3*time.Second, func(time.Time) tea.Msg {
			return clearStatusMsg{}
		}))

	case clearStatusMsg:
		m.statusMsg = ""

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

// findTabIndex returns the index of a tab in the given tab list
func findTabIndex(tabs []Tab, tab Tab) int {
	for i, t := range tabs {
		if t == tab {
			return i
		}
	}
	return 0
}

// questions filters jobs to only return question-type jobs
func (m Model) questions() []*control.JobInfo {
	var result []*control.JobInfo
	for _, j := range m.jobs {
		if j.Type == "question" {
			result = append(result, j)
		}
	}
	return result
}

// showStatus displays a temporary status message
func (m *Model) showStatus(msg string) tea.Cmd {
	m.statusMsg = msg
	m.statusMsgTime = time.Now()
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return clearStatusMsg{}
	})
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
			case TabProjects:
				if m.selected < len(m.projects) {
					project = m.projects[m.selected]
				}
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
			case TabQuestions:
				// Show question detail (questions are jobs with type "question")
				questions := m.questions()
				if m.selected < len(questions) {
					m.detailJob = questions[m.selected]
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
		if m.level == LevelProject {
			switch m.tab {
			case TabAgents:
				agents := m.projectAgents()
				if m.selected < len(agents) {
					m.detailAgent = agents[m.selected]
					m.detailMode = true
					return m, nil
				}
			case TabTasks:
				tasks := m.projectTasks()
				if m.selected < len(tasks) {
					m.detailJob = tasks[m.selected]
					m.detailMode = true
					return m, nil
				}
			}
		}

	case "tab", "l":
		// Tab works at both levels
		if m.level == LevelDashboard {
			idx := findTabIndex(dashboardTabs, m.tab)
			m.tab = dashboardTabs[(idx+1)%len(dashboardTabs)]
			m.selected = 0
		} else {
			idx := findTabIndex(projectTabs, m.tab)
			m.tab = projectTabs[(idx+1)%len(projectTabs)]
			m.selected = 0
		}

	case "shift+tab", "h":
		if m.level == LevelDashboard {
			idx := findTabIndex(dashboardTabs, m.tab)
			m.tab = dashboardTabs[(idx+len(dashboardTabs)-1)%len(dashboardTabs)]
			m.selected = 0
		} else {
			idx := findTabIndex(projectTabs, m.tab)
			m.tab = projectTabs[(idx+len(projectTabs)-1)%len(projectTabs)]
			m.selected = 0
		}

	case "1", "2", "3", "4", "5", "6":
		num := int(msg.String()[0] - '1') // 0-indexed
		if m.level == LevelDashboard {
			if num < len(dashboardTabs) {
				m.tab = dashboardTabs[num]
			}
		} else {
			if num < len(projectTabs) {
				m.tab = projectTabs[num]
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
		// New worktree on worktrees tab
		if m.tab == TabWorktrees {
			m.worktreeMode = true
			m.worktreeStep = 0
			m.worktreeTicketID = ""
			m.worktreeDesc = ""
			m.worktreeProjectIdx = 0
			m.textInput.Placeholder = "Ticket ID (or leave blank)..."
			m.textInput.Reset()
			m.textInput.Focus()
			return m, textarea.Blink
		}
		// New note on notes tab (at any level)
		if m.tab == TabNotes {
			m.inputMode = true
			m.noteMode = true
			m.questionMode = false
			m.textInput.Placeholder = "Enter note..."
			m.textInput.Reset()
			m.textInput.Focus()
			return m, textarea.Blink
		}
		// New job/question at any level (except notes tab)
		m.inputMode = true
		m.questionMode = false
		m.noteMode = false
		m.textInput.Placeholder = "Enter task description..."
		m.textInput.Reset()
		m.textInput.Focus()
		return m, textarea.Blink

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
		return m, textarea.Blink

	case "a":
		if !IsActionAvailable("a", m.tab, m.level) {
			return m, m.showStatus(GetActionTooltip("a", m.tab, m.level))
		}
		m.doAttach()

	case "e":
		if !IsActionAvailable("e", m.tab, m.level) {
			return m, m.showStatus(GetActionTooltip("e", m.tab, m.level))
		}
		m.doOpenNvim()

	case "v":
		if !IsActionAvailable("v", m.tab, m.level) {
			return m, m.showStatus(GetActionTooltip("v", m.tab, m.level))
		}
		m.doView()

	case "p":
		// View implementation plan
		if !IsActionAvailable("p", m.tab, m.level) {
			return m, m.showStatus(GetActionTooltip("p", m.tab, m.level))
		}
		return m, m.doPlanView()

	case "L":
		// View logs for selected agent
		if !IsActionAvailable("L", m.tab, m.level) {
			return m, m.showStatus(GetActionTooltip("L", m.tab, m.level))
		}
		return m, m.doLogs()

	case "s":
		if !IsActionAvailable("s", m.tab, m.level) {
			return m, m.showStatus(GetActionTooltip("s", m.tab, m.level))
		}
		m.doShell()

	case "x", "ctrl+k":
		// Toggle note done on notes tab (at any level)
		if m.tab == TabNotes {
			notes := m.projectNotes()
			if m.selected < len(notes) {
				note := notes[m.selected]
				return m, m.toggleNote(note.ID, !note.Done)
			}
			return m, nil
		}
		return m, m.doKill()

	case " ":
		// Space also toggles note done (at any level)
		if m.tab == TabNotes {
			notes := m.projectNotes()
			if m.selected < len(notes) {
				note := notes[m.selected]
				return m, m.toggleNote(note.ID, !note.Done)
			}
		}

	case "d":
		// Delete note (at project level)
		if m.tab == TabNotes {
			notes := m.projectNotes()
			if m.selected < len(notes) {
				note := notes[m.selected]
				return m, m.deleteNote(note.ID)
			}
		}

	case "f":
		// Promote note to feature/worktree
		if m.tab == TabNotes {
			notes := m.projectNotes()
			if m.selected < len(notes) {
				note := notes[m.selected]
				if len(m.projects) == 0 {
					return m, m.showStatus("No projects available")
				}
				m.promoteMode = true
				m.promoteNoteID = note.ID
				m.promoteNoteText = note.Content
				m.promoteProjectIdx = 0
				return m, nil
			}
		}

	case "r":
		// On agents tab, retry crashed agents; otherwise refresh data
		if m.tab == TabAgents {
			return m, m.doRetry()
		}
		return m, m.fetchData

	case "P":
		// Publish PR (worktrees tab only)
		if m.tab != TabWorktrees {
			return m, m.showStatus("Publish only available on worktrees tab")
		}
		wt := m.getSelectedWorktree()
		if wt == nil {
			return m, m.showStatus("No worktree selected")
		}
		if wt.IsMain {
			return m, m.showStatus("Cannot publish main worktree")
		}
		if wt.WTStatus != "active" && wt.WTStatus != "" {
			return m, m.showStatus("Can only publish active worktrees")
		}
		return m, m.doPublishPR(wt)

	case "M":
		// Merge local (worktrees tab only)
		if m.tab != TabWorktrees {
			return m, m.showStatus("Merge only available on worktrees tab")
		}
		wt := m.getSelectedWorktree()
		if wt == nil {
			return m, m.showStatus("No worktree selected")
		}
		if wt.IsMain {
			return m, m.showStatus("Cannot merge main worktree")
		}
		if wt.WTStatus != "active" && wt.WTStatus != "" {
			return m, m.showStatus("Can only merge active worktrees")
		}
		return m, m.doMergeLocal(wt)

	case "c":
		// Cleanup worktree (worktrees tab only)
		if m.tab != TabWorktrees {
			return m, m.showStatus("Cleanup only available on worktrees tab")
		}
		wt := m.getSelectedWorktree()
		if wt == nil {
			return m, m.showStatus("No worktree selected")
		}
		if wt.IsMain {
			return m, m.showStatus("Cannot cleanup main worktree")
		}
		if wt.WTStatus != "merged" {
			return m, m.showStatus("Can only cleanup merged worktrees (merge first)")
		}
		return m, m.doCleanup(wt)
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
		// Alt+Enter inserts newline
		if msg.Alt {
			m.textInput.InsertString("\n")
			return m, nil
		}

		// Plain Enter submits
		input := strings.TrimSpace(m.textInput.Value())
		if input != "" {
			if m.noteMode {
				// Add note via API
				m.inputMode = false
				m.noteMode = false
				m.textInput.Reset()
				return m, m.createNote(input)
			}
			isQuestion := m.questionMode
			project := m.resolveJobProject()
			if project == "" {
				m.inputMode = false
				m.questionMode = false
				m.textInput.Reset()
				return m, m.showStatus("Select a project before creating a job")
			}
			m.inputMode = false
			m.questionMode = false
			m.textInput.Reset()
			return m, m.createJob(input, isQuestion, project)
		}
		return m, nil

	case "ctrl+j":
		// Ctrl+J is traditional terminal newline
		m.textInput.InsertString("\n")
		return m, nil
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m Model) handleDetailMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "enter":
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
	maxScroll := max(0, len(m.logs)-m.logsViewportHeight())
	halfPage := max(1, m.logsViewportHeight()/2)

	switch msg.String() {
	case "esc", "q":
		m.logsMode = false
		m.logs = nil
		m.logsAgentID = ""
		m.logsScroll = 0
		return m, nil
	case "r":
		// Refresh logs
		return m, m.fetchLogs(m.logsAgentID)

	// Vi-style scrolling
	case "j", "down":
		m.logsFollow = false
		m.logsScroll = min(m.logsScroll+1, maxScroll)
	case "k", "up":
		m.logsFollow = false
		m.logsScroll = max(m.logsScroll-1, 0)
	case "g":
		// Go to top
		m.logsFollow = false
		m.logsScroll = 0
	case "G":
		// Go to bottom
		m.logsFollow = true
		m.logsScroll = maxScroll
	case "ctrl+d":
		// Half page down
		m.logsFollow = false
		m.logsScroll = min(m.logsScroll+halfPage, maxScroll)
	case "ctrl+u":
		// Half page up
		m.logsFollow = false
		m.logsScroll = max(m.logsScroll-halfPage, 0)
	case "f":
		// Toggle follow mode
		m.logsFollow = !m.logsFollow
		if m.logsFollow {
			m.logsScroll = maxScroll
		}
	}
	return m, nil
}

// logsViewportHeight returns available lines for log entries
func (m Model) logsViewportHeight() int {
	// height - header(2) - footer(1) - padding(1)
	return max(5, m.height-4)
}

func (m Model) handlePlanMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	maxScroll := max(0, m.planViewportLines()-m.planViewportHeight())
	halfPage := max(1, m.planViewportHeight()/2)

	switch msg.String() {
	case "esc", "q":
		m.planMode = false
		m.planContent = ""
		m.planRendered = ""
		m.planWorktreePath = ""
		m.planScroll = 0
		return m, nil

	case "a":
		// Approve plan
		if m.planStatus == "draft" {
			return m, m.approvePlan(m.planWorktreePath)
		}
		return m, m.showStatus("Plan already approved")

	case "x":
		// Execute plan (spawn executor)
		if m.planStatus == "approved" {
			m.planMode = false
			return m, m.spawnExecutor(m.planWorktreePath)
		}
		return m, m.showStatus("Approve plan first with [a]")

	case "r":
		// Refresh plan from file
		return m, m.fetchPlan(m.planWorktreePath, true)

	case "L":
		// View planner agent logs (especially useful when pending)
		if m.planAgentID != "" {
			m.planMode = false
			return m, m.fetchLogs(m.planAgentID)
		}
		return m, m.showStatus("No planner agent to view logs for")

	// Vi-style scrolling
	case "j", "down":
		m.planScroll = min(m.planScroll+1, maxScroll)
	case "k", "up":
		m.planScroll = max(m.planScroll-1, 0)
	case "g":
		m.planScroll = 0
	case "G":
		m.planScroll = maxScroll
	case "ctrl+d":
		m.planScroll = min(m.planScroll+halfPage, maxScroll)
	case "ctrl+u":
		m.planScroll = max(m.planScroll-halfPage, 0)
	}
	return m, nil
}

// planViewportHeight returns available lines for plan content
func (m Model) planViewportHeight() int {
	// height - header(3) - footer(2) - padding(2)
	return max(5, m.height-7)
}

// planViewportLines returns total lines in rendered plan
func (m Model) planViewportLines() int {
	return strings.Count(m.planRendered, "\n") + 1
}

// renderMarkdown renders markdown content using glamour
func (m Model) renderMarkdown(content string) string {
	width := m.width - 4
	if width < 40 {
		width = 40
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return content // fallback to raw
	}

	out, err := r.Render(content)
	if err != nil {
		return content // fallback to raw
	}
	return out
}

func (m Model) handleWorktreeWizard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Cancel wizard
		m.worktreeMode = false
		m.textInput.Reset()
		return m, nil

	case "enter":
		switch m.worktreeStep {
		case 0: // Ticket ID step
			ticketID := strings.TrimSpace(m.textInput.Value())
			m.worktreeTicketID = ticketID
			m.textInput.Reset()

			if ticketID == "" {
				// No ticket - prompt for description
				m.worktreeStep = 1
				m.textInput.Placeholder = "Description for worktree..."
				m.textInput.Focus()
				return m, textarea.Blink
			}
			// Has ticket - skip to project selection
			m.worktreeStep = 2
			return m, nil

		case 1: // Description step (only if no ticket)
			desc := strings.TrimSpace(m.textInput.Value())
			if desc == "" {
				// Require description if no ticket
				return m, m.showStatus("Description required when no ticket ID")
			}
			m.worktreeDesc = desc
			m.textInput.Reset()
			m.worktreeStep = 2
			return m, nil

		case 2: // Project selection step
			if len(m.projects) == 0 {
				return m, m.showStatus("No projects available")
			}
			// Create the worktree
			project := m.projects[m.worktreeProjectIdx]
			m.worktreeMode = false

			return m, m.createWorktreeCmd(project, m.worktreeTicketID, m.worktreeDesc)
		}

	case "j", "down":
		// In project selection, move down
		if m.worktreeStep == 2 {
			if m.worktreeProjectIdx < len(m.projects)-1 {
				m.worktreeProjectIdx++
			}
			return m, nil
		}

	case "k", "up":
		// In project selection, move up
		if m.worktreeStep == 2 {
			if m.worktreeProjectIdx > 0 {
				m.worktreeProjectIdx--
			}
			return m, nil
		}

	case "tab":
		// Tab skips ticket entry with empty value
		if m.worktreeStep == 0 {
			m.worktreeTicketID = ""
			m.textInput.Reset()
			m.worktreeStep = 1
			m.textInput.Placeholder = "Description for worktree..."
			m.textInput.Focus()
			return m, textarea.Blink
		}
	}

	// Forward to text input for steps 0 and 1
	if m.worktreeStep < 2 {
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) createWorktreeCmd(project, ticketID, description string) tea.Cmd {
	return func() tea.Msg {
		// Find the main repo path for this project
		var mainRepoPath string
		for _, wt := range m.worktrees {
			if wt.Project == project && wt.IsMain {
				mainRepoPath = wt.Path
				break
			}
		}
		if mainRepoPath == "" {
			// Use first worktree for this project as fallback
			for _, wt := range m.worktrees {
				if wt.Project == project {
					mainRepoPath = wt.Path
					break
				}
			}
		}
		if mainRepoPath == "" {
			return errMsg(fmt.Errorf("no worktree found for project %s", project))
		}

		_, err := m.client.CreateWorktree(control.CreateWorktreeRequest{
			MainRepoPath: mainRepoPath,
			TicketID:     ticketID,
			Description:  description,
		})
		if err != nil {
			return errMsg(err)
		}
		return dataUpdateMsg{}
	}
}

func (m Model) handlePromoteNote(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Cancel promotion
		m.promoteMode = false
		m.promoteNoteID = ""
		m.promoteNoteText = ""
		return m, nil

	case "enter":
		// Promote note to worktree
		if len(m.projects) == 0 {
			return m, m.showStatus("No projects available")
		}
		project := m.projects[m.promoteProjectIdx]
		noteID := m.promoteNoteID
		noteText := m.promoteNoteText
		m.promoteMode = false

		return m, m.promoteNoteCmd(project, noteID, noteText)

	case "j", "down":
		if m.promoteProjectIdx < len(m.projects)-1 {
			m.promoteProjectIdx++
		}
		return m, nil

	case "k", "up":
		if m.promoteProjectIdx > 0 {
			m.promoteProjectIdx--
		}
		return m, nil
	}

	return m, nil
}

func (m Model) promoteNoteCmd(project, noteID, noteText string) tea.Cmd {
	return func() tea.Msg {
		// Find the main repo path for this project
		var mainRepoPath string
		for _, wt := range m.worktrees {
			if wt.Project == project && wt.IsMain {
				mainRepoPath = wt.Path
				break
			}
		}
		if mainRepoPath == "" {
			for _, wt := range m.worktrees {
				if wt.Project == project {
					mainRepoPath = wt.Path
					break
				}
			}
		}
		if mainRepoPath == "" {
			return errMsg(fmt.Errorf("no worktree found for project %s", project))
		}

		// Create worktree with note as description
		_, err := m.client.CreateWorktree(control.CreateWorktreeRequest{
			MainRepoPath: mainRepoPath,
			TicketID:     "", // No ticket for promoted notes
			Description:  noteText,
		})
		if err != nil {
			return errMsg(err)
		}

		// Mark note as done (rather than deleting to preserve history)
		_ = m.client.UpdateNote(control.UpdateNoteRequest{
			ID:   noteID,
			Done: true,
		})

		return dataUpdateMsg{}
	}
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
		case TabProjects:
			return max(0, len(m.projects)-1)
		case TabWorktrees:
			return max(0, len(m.worktrees)-1)
		case TabJobs:
			return max(0, len(m.jobs)-1)
		case TabQuestions:
			return max(0, len(m.questions())-1)
		}
		return 0
	}

	// Project level - filtered lists
	switch m.tab {
	case TabWorktrees:
		return max(0, len(m.projectWorktrees())-1)
	case TabAgents:
		return max(0, len(m.projectAgents())-1)
	case TabTasks:
		return max(0, len(m.projectTasks())-1)
	case TabNotes:
		return max(0, len(m.projectNotes())-1)
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

// getSelectedWorktree returns the currently selected worktree, or nil if none.
func (m Model) getSelectedWorktree() *control.WorktreeInfo {
	var wts []*control.WorktreeInfo
	if m.level == LevelDashboard {
		wts = m.worktrees
	} else {
		wts = m.projectWorktrees()
	}
	if m.selected >= len(wts) {
		return nil
	}
	return wts[m.selected]
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

func (m Model) projectTasks() []*control.JobInfo {
	// Tasks are short-lived jobs for this project
	var result []*control.JobInfo
	for _, j := range m.jobs {
		if j.Project == m.selectedProject && j.Type != "question" {
			result = append(result, j)
		}
	}
	return result
}

func (m Model) projectNotes() []*control.NoteInfo {
	// For now, notes are global - in future could be project-specific
	// Filter by project if notes have project field, otherwise return all
	notes := make([]*control.NoteInfo, 0, len(m.notes))
	for _, note := range m.notes {
		if !note.Done {
			notes = append(notes, note)
		}
	}
	return notes
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

	// Plan viewer overlay
	if m.planMode {
		return m.renderPlanView()
	}

	// Worktree creation wizard
	if m.worktreeMode {
		return m.renderWorktreeWizard()
	}

	// Note promotion wizard
	if m.promoteMode {
		return m.renderPromoteWizard()
	}

	var b strings.Builder

	if m.level == LevelDashboard {
		b.WriteString(m.renderDashboard())
	} else {
		b.WriteString(m.renderProjectDetail())
	}

	// Input overlay - pinned to bottom
	if m.inputMode {
		content := b.String()
		return m.applyInputOverlay(content)
	}

	return b.String()
}

func (m Model) renderDashboard() string {
	var content strings.Builder

	// Header with icon, title, and tabs
	content.WriteString(m.renderHeader())
	content.WriteString("\n")

	// Divider
	content.WriteString(tui.Divider(m.width))
	content.WriteString("\n")

	// Breathing room
	content.WriteString("\n")

	// Content based on current tab
	content.WriteString(m.renderDashboardContent())

	// Pin footer to bottom
	footer := m.renderFooter()
	return layout.PinFooterToBottom(content.String(), footer, m.height)
}

func (m Model) renderDaemonStatus() string {
	if m.client != nil && m.client.Connected() {
		return tui.StyleSuccess.Render("* daemon")
	}
	return tui.StyleDanger.Render("x daemon")
}

func (m Model) renderHeader() string {
	// Logo + title
	logo := tui.Logo()
	title := tui.StyleLogo.Render(" athena")

	// Tab names for display
	tabNames := map[Tab]string{
		TabProjects:  "projects",
		TabWorktrees: "worktrees",
		TabJobs:      "jobs",
		TabQuestions: "questions",
		TabAgents:    "agents",
		TabTasks:     "tasks",
		TabNotes:     "notes",
	}

	// Tabs at dashboard level (worktrees, agents, questions, notes)
	tabs := dashboardTabs
	var tabList []struct {
		name string
		tab  Tab
	}
	for _, t := range tabs {
		tabList = append(tabList, struct {
			name string
			tab  Tab
		}{tabNames[t], t})
	}

	var tabParts []string
	for _, t := range tabList {
		if t.tab == m.tab {
			tabParts = append(tabParts, tui.StyleTabActive.Render(t.name))
		} else {
			tabParts = append(tabParts, tui.StyleTabInactive.Render(t.name))
		}
	}
	tabStr := strings.Join(tabParts, "  ")

	// Stats - count agents needing attention
	needsAttention := 0
	activeCount := 0
	for _, a := range m.agents {
		if a.Status == "running" || a.Status == "planning" || a.Status == "executing" {
			activeCount++
		}
		if a.Status == "awaiting" || a.Status == "planning" {
			needsAttention++
		}
	}

	var stats string
	if needsAttention > 0 {
		// Amber warning when agents need attention
		stats = tui.StyleWarning.Render(fmt.Sprintf("%d need attention", needsAttention)) +
			tui.StyleMuted.Render(" │ ") +
			tui.StyleStatus.Render(fmt.Sprintf("%d/%d agents", activeCount, len(m.agents)))
	} else {
		stats = tui.StyleStatus.Render(fmt.Sprintf("%d/%d agents", activeCount, len(m.agents)))
	}
	stats = m.renderDaemonStatus() + tui.StyleMuted.Render(" │ ") + stats

	// Layout
	sep := tui.StyleMuted.Render(" │ ")
	left := logo + title + sep + tabStr
	padding := m.width - lipgloss.Width(left) - lipgloss.Width(stats)
	padding = max(1, padding)
	return left + strings.Repeat(" ", padding) + stats
}

func (m Model) renderFooter() string {
	// Show error if present (highest priority)
	if m.err != nil {
		errStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f7768e")).
			Bold(true)
		return errStyle.Render(fmt.Sprintf(" ✗ %s", m.err.Error()))
	}

	// Show status message if recent
	if m.statusMsg != "" && time.Since(m.statusMsgTime) < 2*time.Second {
		return tui.StyleStatusMsg.Render(m.statusMsg)
	}

	// Build context-aware help
	help := FormatHelp(m.tab, m.level)

	// Right-aligned timestamp
	update := fmt.Sprintf("%s ago", formatDuration(time.Since(m.lastUpdate)))

	footerPad := m.width - lipgloss.Width(help) - lipgloss.Width(update) - 2
	if footerPad < 1 {
		footerPad = 1
	}
	return tui.StyleHelp.Render(help) + strings.Repeat(" ", footerPad) + tui.StyleStatus.Render(update)
}

func (m Model) renderDashboardContent() string {
	switch m.tab {
	case TabProjects:
		return m.renderProjects()
	case TabWorktrees:
		return m.renderAllWorktrees()
	case TabJobs:
		return m.renderAllJobs()
	case TabQuestions:
		return m.renderQuestions()
	}
	return ""
}

func (m Model) renderProjects() string {
	var b strings.Builder

	contentHeight := layout.ContentHeight(m.height)

	// Projects table - overview of all projects
	projectTable := layout.NewTable([]layout.Column{
		{Header: "PROJECT", MinWidth: 15, MaxWidth: 30, Flex: 2},
		{Header: "WORKTREES", MinWidth: 10, MaxWidth: 12, Flex: 0},
		{Header: "AGENTS", MinWidth: 8, MaxWidth: 10, Flex: 0},
		{Header: "STATUS", MinWidth: 12, MaxWidth: 18, Flex: 1},
	})
	projectTable.SetWidth(m.width)

	b.WriteString(projectTable.RenderHeader())
	b.WriteString("\n\n")

	if len(m.projects) == 0 {
		b.WriteString(tui.StyleEmptyState.Render("   No projects found. Configure base_dirs in ~/.config/athena/config.yaml"))
		b.WriteString("\n")
		for i := 1; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	// Calculate scroll window
	scroll := layout.CalculateScrollWindow(len(m.projects), m.selected, contentHeight-1)

	// Scroll indicator top
	if scroll.HasLess {
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▲ %d more", scroll.Offset)))
		b.WriteString("\n")
	}

	// Render visible rows
	end := scroll.Offset + scroll.VisibleRows
	if end > len(m.projects) {
		end = len(m.projects)
	}

	for i := scroll.Offset; i < end; i++ {
		project := m.projects[i]
		row := m.renderProjectRow(project, projectTable, i == m.selected)
		b.WriteString(row)
		b.WriteString("\n")
	}

	// Scroll indicator bottom
	if scroll.HasMore {
		remaining := len(m.projects) - end
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▼ %d more", remaining)))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderProjectRow(project string, table *layout.Table, selected bool) string {
	// Count worktrees for this project
	wtCount := 0
	for _, wt := range m.worktrees {
		if wt.Project == project {
			wtCount++
		}
	}

	// Count agents for this project
	agentCount := 0
	runningCount := 0
	for _, a := range m.agents {
		if a.Project == project {
			agentCount++
			if a.Status == "running" || a.Status == "planning" || a.Status == "executing" {
				runningCount++
			}
		}
	}

	// Determine status summary
	status := tui.StyleMuted.Render("idle")
	if runningCount > 0 {
		status = tui.StyleSuccess.Render(fmt.Sprintf("%d running", runningCount))
	} else if agentCount > 0 {
		status = tui.StyleNeutral.Render("agents idle")
	}

	values := []string{
		project,
		fmt.Sprintf("%d", wtCount),
		fmt.Sprintf("%d", agentCount),
		status,
	}

	return table.RenderRow(values, selected)
}

func (m Model) renderAllWorktrees() string {
	var b strings.Builder

	contentHeight := layout.ContentHeight(m.height)

	// Column headers
	b.WriteString(m.worktreeTable.RenderHeader())
	b.WriteString("\n\n")

	if len(m.worktrees) == 0 {
		b.WriteString(tui.StyleEmptyState.Render("   No worktrees found. Configure base_dirs in ~/.config/athena/config.yaml"))
		b.WriteString("\n")
		for i := 1; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	// Calculate scroll window
	scroll := layout.CalculateScrollWindow(len(m.worktrees), m.selected, contentHeight-1)

	// Scroll indicator top
	if scroll.HasLess {
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▲ %d more", scroll.Offset)))
		b.WriteString("\n")
	}

	// Render visible rows
	end := scroll.Offset + scroll.VisibleRows
	if end > len(m.worktrees) {
		end = len(m.worktrees)
	}

	for i := scroll.Offset; i < end; i++ {
		wt := m.worktrees[i]
		row := m.renderGlobalWorktreeRow(wt, i == m.selected)
		b.WriteString(row)
		b.WriteString("\n")
	}

	// Scroll indicator bottom
	if scroll.HasMore {
		remaining := len(m.worktrees) - end
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▼ %d more", remaining)))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderGlobalWorktreeRow(wt *control.WorktreeInfo, selected bool) string {
	// Determine the status icon based on worktree lifecycle and agent status
	icon := tui.StyleMuted.Render("─")

	// Check worktree lifecycle status first
	if wt.WTStatus == "merged" {
		icon = tui.StyleNeutral.Render("✓") // Merged - can be deleted
	} else if wt.WTStatus == "stale" {
		icon = tui.StyleWarning.Render("!") // Stale - needs attention
	} else if wt.AgentID != "" {
		// Active worktree with agent - show agent status
		for _, a := range m.agents {
			if a.ID == wt.AgentID {
				icon = tui.StatusStyle(a.Status).Render(tui.StatusIcons[a.Status])
				break
			}
		}
	}

	path := filepath.Base(wt.Path)
	if wt.IsMain {
		path = path + " (main)"
	}

	values := []string{
		wt.Project,
		path,
		wt.Branch,
		icon,
		wt.Status,
	}

	row := m.worktreeTable.RenderRow(values, selected)

	// Render merged worktrees in muted style (unless selected)
	if wt.WTStatus == "merged" && !selected {
		return tui.StyleMuted.Render(row)
	}

	return row
}

func (m Model) renderAllJobs() string {
	var b strings.Builder

	contentHeight := layout.ContentHeight(m.height)

	// Column headers
	b.WriteString(m.jobTable.RenderHeader())
	b.WriteString("\n\n")

	if len(m.jobs) == 0 {
		b.WriteString(tui.StyleEmptyState.Render("   No jobs. Enter a project and press [n] to create one."))
		b.WriteString("\n")
		for i := 1; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	// Calculate scroll window
	scroll := layout.CalculateScrollWindow(len(m.jobs), m.selected, contentHeight-1)

	// Scroll indicator top
	if scroll.HasLess {
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▲ %d more", scroll.Offset)))
		b.WriteString("\n")
	}

	// Render visible rows
	end := scroll.Offset + scroll.VisibleRows
	if end > len(m.jobs) {
		end = len(m.jobs)
	}

	for i := scroll.Offset; i < end; i++ {
		job := m.jobs[i]
		row := m.renderGlobalJobRow(job, i, i == m.selected)
		b.WriteString(row)
		b.WriteString("\n")
	}

	// Scroll indicator bottom
	if scroll.HasMore {
		remaining := len(m.jobs) - end
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▼ %d more", remaining)))
		b.WriteString("\n")
	}

	// Fill remaining space
	return b.String()
}

func (m Model) renderGlobalJobRow(job *control.JobInfo, idx int, selected bool) string {
	agentIcon := tui.StyleMuted.Render("─")
	if job.AgentID != "" {
		for _, a := range m.agents {
			if a.ID == job.AgentID {
				agentIcon = tui.StatusStyle(a.Status).Render(tui.StatusIcons[a.Status])
				break
			}
		}
	}

	values := []string{
		fmt.Sprintf("%d", idx+1),
		job.Project,
		job.NormalizedInput,
		job.Status,
		agentIcon,
	}

	return m.jobTable.RenderRow(values, selected)
}

func (m Model) renderQuestions() string {
	var b strings.Builder
	questions := m.questions()

	contentHeight := layout.ContentHeight(m.height)

	// Questions table - simpler than jobs, focused on Q&A
	questionTable := layout.NewTable([]layout.Column{
		{Header: "ST", MinWidth: 2, MaxWidth: 2, Flex: 0},
		{Header: "QUESTION", MinWidth: 40, MaxWidth: 0, Flex: 4},
		{Header: "AGE", MinWidth: 5, MaxWidth: 8, Flex: 0},
	})
	questionTable.SetWidth(m.width)

	b.WriteString(questionTable.RenderHeader())
	b.WriteString("\n\n")

	if len(questions) == 0 {
		b.WriteString(tui.StyleEmptyState.Render("   No questions. Press [?] to ask a quick question."))
		b.WriteString("\n")
		for i := 1; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	// Calculate scroll window
	scroll := layout.CalculateScrollWindow(len(questions), m.selected, contentHeight-1)

	// Scroll indicator top
	if scroll.HasLess {
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▲ %d more", scroll.Offset)))
		b.WriteString("\n")
	}

	// Render visible rows
	end := scroll.Offset + scroll.VisibleRows
	if end > len(questions) {
		end = len(questions)
	}

	for i := scroll.Offset; i < end; i++ {
		q := questions[i]
		row := m.renderQuestionRow(q, questionTable, i == m.selected)
		b.WriteString(row)
		b.WriteString("\n")
	}

	// Scroll indicator bottom
	if scroll.HasMore {
		remaining := len(questions) - end
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▼ %d more", remaining)))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderQuestionRow(job *control.JobInfo, table *layout.Table, selected bool) string {
	// Status icon
	icon := tui.StatusStyle(job.Status).Render(tui.StatusIcons[job.Status])

	// Calculate age from created_at
	age := ""
	if t, err := time.Parse(time.RFC3339, job.CreatedAt); err == nil {
		age = formatDuration(time.Since(t))
	}

	values := []string{
		icon,
		job.NormalizedInput,
		age,
	}

	return table.RenderRow(values, selected)
}

func (m Model) renderProjectDetail() string {
	var content strings.Builder

	// Header with project name and tabs
	content.WriteString(m.renderDetailHeader())
	content.WriteString("\n")

	// Divider
	content.WriteString(tui.Divider(m.width))
	content.WriteString("\n")

	// Breathing room
	content.WriteString("\n")

	// Content
	content.WriteString(m.renderDetailContent())

	// Pin footer to bottom
	footer := m.renderDetailFooter()
	return layout.PinFooterToBottom(content.String(), footer, m.height)
}

func (m Model) renderDetailHeader() string {
	// Icon and project name
	logo := tui.Logo()
	proj := logo + " " + tui.StyleLogo.Render(m.selectedProject)

	// Tabs for project drill-in
	tabs := []struct {
		name string
		tab  Tab
	}{
		{"worktrees", TabWorktrees},
		{"agents", TabAgents},
		{"tasks", TabTasks},
		{"notes", TabNotes},
	}

	var tabParts []string
	for _, t := range tabs {
		if t.tab == m.tab {
			tabParts = append(tabParts, tui.StyleTabActive.Render(t.name))
		} else {
			tabParts = append(tabParts, tui.StyleTabInactive.Render(t.name))
		}
	}
	tabStr := strings.Join(tabParts, "  ")

	// Stats
	agents := m.projectAgents()
	activeCount := 0
	for _, a := range agents {
		if a.Status == "running" || a.Status == "planning" || a.Status == "executing" {
			activeCount++
		}
	}
	stats := m.renderDaemonStatus() + tui.StyleMuted.Render(" │ ") +
		tui.StyleStatus.Render(fmt.Sprintf("%d/%d", activeCount, len(agents)))

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
	case TabAgents:
		return m.renderAgents()
	case TabTasks:
		return m.renderTasks()
	case TabNotes:
		return m.renderNotes()
	}
	return ""
}

func (m Model) renderWorktrees() string {
	var b strings.Builder
	wts := m.projectWorktrees()

	contentHeight := layout.ContentHeight(m.height)

	// Column headers - project-level uses different columns (no PROJECT column)
	projectTable := layout.NewTable([]layout.Column{
		{Header: "PATH", MinWidth: 25, MaxWidth: 40, Flex: 2},
		{Header: "BRANCH", MinWidth: 15, MaxWidth: 25, Flex: 1},
		{Header: "ST", MinWidth: 2, MaxWidth: 2, Flex: 0},
		{Header: "GIT", MinWidth: 3, MaxWidth: 8, Flex: 0},
	})
	projectTable.SetWidth(m.width)

	b.WriteString(projectTable.RenderHeader())
	b.WriteString("\n\n")

	if len(wts) == 0 {
		b.WriteString(tui.StyleEmptyState.Render("   No worktrees for this project."))
		b.WriteString("\n")
		for i := 1; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	// Calculate scroll window
	scroll := layout.CalculateScrollWindow(len(wts), m.selected, contentHeight-1)

	// Scroll indicator top
	if scroll.HasLess {
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▲ %d more", scroll.Offset)))
		b.WriteString("\n")
	}

	// Render visible rows
	end := scroll.Offset + scroll.VisibleRows
	if end > len(wts) {
		end = len(wts)
	}

	for i := scroll.Offset; i < end; i++ {
		wt := wts[i]
		row := m.renderWorktreeRow(wt, projectTable, i == m.selected)
		b.WriteString(row)
		b.WriteString("\n")
	}

	// Scroll indicator bottom
	if scroll.HasMore {
		remaining := len(wts) - end
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▼ %d more", remaining)))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderWorktreeRow(wt *control.WorktreeInfo, table *layout.Table, selected bool) string {
	// Determine the status icon based on worktree lifecycle and agent status
	icon := tui.StyleMuted.Render("─")

	// Check worktree lifecycle status first
	if wt.WTStatus == "merged" {
		icon = tui.StyleNeutral.Render("✓") // Merged - can be deleted
	} else if wt.WTStatus == "stale" {
		icon = tui.StyleWarning.Render("!") // Stale - needs attention
	} else if wt.AgentID != "" {
		// Active worktree with agent - show agent status
		for _, a := range m.agents {
			if a.ID == wt.AgentID {
				icon = tui.StatusStyle(a.Status).Render(tui.StatusIcons[a.Status])
				break
			}
		}
	}

	path := filepath.Base(wt.Path)
	if wt.IsMain {
		path = path + " (main)"
	}

	values := []string{
		path,
		wt.Branch,
		icon,
		wt.Status,
	}

	row := table.RenderRow(values, selected)

	// Render merged worktrees in muted style (unless selected)
	if wt.WTStatus == "merged" && !selected {
		return tui.StyleMuted.Render(row)
	}

	return row
}

func (m Model) renderAgents() string {
	var b strings.Builder
	agents := m.projectAgents()

	contentHeight := layout.ContentHeight(m.height)

	// Project-level agent table (no PROJECT column, Notes-style layout for activity)
	projectAgentTable := layout.NewTable([]layout.Column{
		{Header: "ST", MinWidth: 2, MaxWidth: 2, Flex: 0},
		{Header: "TYPE", MinWidth: 8, MaxWidth: 12, Flex: 0},
		{Header: "WORKTREE", MinWidth: 18, MaxWidth: 30, Flex: 1},
		{Header: "ACTIVITY", MinWidth: 30, MaxWidth: 80, Flex: 3}, // What the agent is doing
		{Header: "AGE", MinWidth: 5, MaxWidth: 8, Flex: 0},
	})
	projectAgentTable.SetWidth(m.width)

	b.WriteString(projectAgentTable.RenderHeader())
	b.WriteString("\n\n")

	if len(agents) == 0 {
		b.WriteString(tui.StyleEmptyState.Render("   No agents running."))
		b.WriteString("\n")
		for i := 1; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	// Calculate scroll window
	scroll := layout.CalculateScrollWindow(len(agents), m.selected, contentHeight-1)

	// Scroll indicator top
	if scroll.HasLess {
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▲ %d more", scroll.Offset)))
		b.WriteString("\n")
	}

	// Render visible rows
	end := scroll.Offset + scroll.VisibleRows
	if end > len(agents) {
		end = len(agents)
	}

	for i := scroll.Offset; i < end; i++ {
		agent := agents[i]
		row := m.renderAgentRow(agent, projectAgentTable, i == m.selected)
		b.WriteString(row)
		b.WriteString("\n")
	}

	// Scroll indicator bottom
	if scroll.HasMore {
		remaining := len(agents) - end
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▼ %d more", remaining)))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderAgentRow(agent *control.AgentInfo, table *layout.Table, selected bool) string {
	icon := tui.StatusStyle(agent.Status).Render(tui.StatusIcons[agent.Status])
	wtName := filepath.Base(agent.WorktreePath)
	age := formatDuration(time.Since(parseCreatedAt(agent.CreatedAt)))

	values := []string{
		icon,
		agent.Archetype,
		wtName,
		formatActivity(agent),
		age,
	}

	return table.RenderRow(values, selected)
}

// formatActivity returns a human-readable description of what the agent is doing.
// If LastActivity is populated, it shows that with a relative time suffix.
// Otherwise, it falls back to descriptive status messages.
func formatActivity(agent *control.AgentInfo) string {
	if agent.LastActivity != "" {
		// Show activity with relative time if recent
		if agent.LastActivityTime != "" {
			activityTime := parseCreatedAt(agent.LastActivityTime)
			if !activityTime.IsZero() && time.Since(activityTime) < 5*time.Minute {
				relTime := formatDuration(time.Since(activityTime))
				return fmt.Sprintf("%s (%s ago)", agent.LastActivity, relTime)
			}
		}
		return agent.LastActivity
	}

	// Fallback to status-based descriptions
	switch agent.Status {
	case "awaiting":
		return "Waiting for user input"
	case "crashed":
		return "Process crashed - needs restart"
	case "planning":
		return "Creating implementation plan..."
	case "executing":
		return "Executing implementation..."
	case "running":
		return "Working..."
	case "stopped":
		return "Stopped"
	default:
		return agent.Status
	}
}

func (m Model) renderNotes() string {
	var b strings.Builder

	contentHeight := layout.ContentHeight(m.height)
	notes := m.projectNotes()

	// Notes table - no MaxWidth so notes never truncate
	noteTable := layout.NewTable([]layout.Column{
		{Header: "✓", MinWidth: 3, MaxWidth: 3, Flex: 0},
		{Header: "NOTE", MinWidth: 45, MaxWidth: 0, Flex: 3}, // MaxWidth 0 = unlimited
	})
	noteTable.SetWidth(m.width)

	b.WriteString(noteTable.RenderHeader())
	b.WriteString("\n\n")

	if len(notes) == 0 {
		b.WriteString(tui.StyleEmptyState.Render("   No active notes. Quick ideas and todos go here."))
		b.WriteString("\n")
		for i := 1; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	// Calculate scroll window
	scroll := layout.CalculateScrollWindow(len(notes), m.selected, contentHeight-1)

	// Scroll indicator top
	if scroll.HasLess {
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▲ %d more", scroll.Offset)))
		b.WriteString("\n")
	}

	// Render visible rows
	end := scroll.Offset + scroll.VisibleRows
	if end > len(notes) {
		end = len(notes)
	}

	for i := scroll.Offset; i < end; i++ {
		note := notes[i]
		row := m.renderNoteRow(note, noteTable, i == m.selected)
		b.WriteString(row)
		b.WriteString("\n")
	}

	// Scroll indicator bottom
	if scroll.HasMore {
		remaining := len(notes) - end
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▼ %d more", remaining)))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderNoteRow(note *control.NoteInfo, table *layout.Table, selected bool) string {
	check := "○"
	if note.Done {
		check = "●"
	}

	values := []string{
		check,
		note.Content,
	}

	row := table.RenderRow(values, selected)
	if note.Done && !selected {
		return tui.StyleMuted.Render(row)
	}
	return row
}

func (m Model) renderTasks() string {
	var b strings.Builder
	tasks := m.projectTasks()

	contentHeight := layout.ContentHeight(m.height)

	// Tasks table (short-lived jobs)
	taskTable := layout.NewTable([]layout.Column{
		{Header: "ST", MinWidth: 2, MaxWidth: 2, Flex: 0},
		{Header: "TASK", MinWidth: 35, MaxWidth: 60, Flex: 3},
		{Header: "STATUS", MinWidth: 10, MaxWidth: 12, Flex: 0},
		{Header: "AGE", MinWidth: 5, MaxWidth: 8, Flex: 0},
	})
	taskTable.SetWidth(m.width)

	b.WriteString(taskTable.RenderHeader())
	b.WriteString("\n\n")

	if len(tasks) == 0 {
		b.WriteString(tui.StyleEmptyState.Render("   No tasks. Press [n] to create one."))
		b.WriteString("\n")
		for i := 1; i < contentHeight-1; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}

	// Calculate scroll window
	scroll := layout.CalculateScrollWindow(len(tasks), m.selected, contentHeight-1)

	// Scroll indicator top
	if scroll.HasLess {
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▲ %d more", scroll.Offset)))
		b.WriteString("\n")
	}

	// Render visible rows
	end := scroll.Offset + scroll.VisibleRows
	if end > len(tasks) {
		end = len(tasks)
	}

	for i := scroll.Offset; i < end; i++ {
		task := tasks[i]
		row := m.renderTaskRow(task, taskTable, i == m.selected)
		b.WriteString(row)
		b.WriteString("\n")
	}

	// Scroll indicator bottom
	if scroll.HasMore {
		remaining := len(tasks) - end
		b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("   ▼ %d more", remaining)))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderTaskRow(task *control.JobInfo, table *layout.Table, selected bool) string {
	icon := tui.StatusStyle(task.Status).Render(tui.StatusIcons[task.Status])

	age := ""
	if t, err := time.Parse(time.RFC3339, task.CreatedAt); err == nil {
		age = formatDuration(time.Since(t))
	}

	values := []string{
		icon,
		task.NormalizedInput,
		task.Status,
		age,
	}

	return table.RenderRow(values, selected)
}

func (m Model) renderJobDetail() string {
	var content strings.Builder
	job := m.detailJob

	// Header
	icon := tui.Logo()
	typeLabel := "Job"
	if job.Type == "question" {
		typeLabel = "Question"
	}
	content.WriteString(icon + " " + tui.StyleLogo.Render(typeLabel))
	content.WriteString(tui.StyleMuted.Render(" │ " + job.Status))
	content.WriteString("\n")
	content.WriteString(tui.Divider(m.width))
	content.WriteString("\n\n")

	// Task/Question
	content.WriteString(tui.StyleMuted.Render("  Task: "))
	content.WriteString(job.NormalizedInput)
	content.WriteString("\n\n")

	// Answer (for question jobs)
	if job.Type == "question" {
		content.WriteString(tui.StyleMuted.Render("  Answer:\n"))
		if job.Answer != "" {
			// Word wrap the answer
			lines := wrapText(job.Answer, m.width-4)
			for _, line := range lines {
				content.WriteString("  " + line + "\n")
			}
		} else if job.Status == "pending" || job.Status == "executing" {
			content.WriteString(tui.StyleMuted.Render("  Processing...\n"))
		} else {
			content.WriteString(tui.StyleMuted.Render("  No answer yet.\n"))
		}
	} else {
		// For feature jobs show more info
		if job.AgentID != "" {
			content.WriteString(tui.StyleMuted.Render("  Agent: "))
			content.WriteString(job.AgentID[:8] + "...")
			content.WriteString("\n")
		}
		if job.WorktreePath != "" {
			content.WriteString(tui.StyleMuted.Render("  Worktree: "))
			content.WriteString(job.WorktreePath)
			content.WriteString("\n")
		}
	}

	// Pin footer to bottom
	footer := tui.StyleHelp.Render("  Press Esc or Enter to close")
	return layout.PinFooterToBottom(content.String(), footer, m.height)
}

func (m Model) renderAgentDetail() string {
	var content strings.Builder
	agent := m.detailAgent

	// Header with icon and status
	icon := tui.StatusStyle(agent.Status).Render(tui.StatusIcons[agent.Status])
	content.WriteString(icon + " ")
	content.WriteString(tui.StyleLogo.Render("Agent"))
	content.WriteString(tui.StyleMuted.Render(" │ "))
	content.WriteString(tui.StatusStyle(agent.Status).Render(agent.Status))
	content.WriteString("\n")
	content.WriteString(tui.Divider(m.width))
	content.WriteString("\n\n")

	// Key info
	content.WriteString(tui.StyleMuted.Render("  ID:        "))
	content.WriteString(agent.ID)
	content.WriteString("\n")

	content.WriteString(tui.StyleMuted.Render("  Project:   "))
	content.WriteString(agent.ProjectName)
	content.WriteString("\n")

	content.WriteString(tui.StyleMuted.Render("  Archetype: "))
	content.WriteString(agent.Archetype)
	content.WriteString("\n")

	content.WriteString(tui.StyleMuted.Render("  Worktree:  "))
	content.WriteString(agent.WorktreePath)
	content.WriteString("\n")

	if agent.LinearIssueID != "" {
		content.WriteString(tui.StyleMuted.Render("  Ticket:    "))
		content.WriteString(tui.StyleAccent.Render(agent.LinearIssueID))
		content.WriteString("\n")
	}

	age := formatDuration(time.Since(parseCreatedAt(agent.CreatedAt)))
	content.WriteString(tui.StyleMuted.Render("  Age:       "))
	content.WriteString(age)
	content.WriteString("\n")

	content.WriteString(tui.StyleMuted.Render("  Restarts:  "))
	content.WriteString(fmt.Sprintf("%d", agent.RestartCount))
	content.WriteString("\n")

	// Usage metrics
	if agent.Metrics != nil {
		content.WriteString("\n")
		content.WriteString(tui.StyleMuted.Render("  Usage:\n"))
		content.WriteString(fmt.Sprintf("    Tools:    %d calls\n", agent.Metrics.ToolUseCount))
		content.WriteString(fmt.Sprintf("    Files:    %d read, %d written\n", agent.Metrics.FilesRead, agent.Metrics.FilesWritten))
		content.WriteString(fmt.Sprintf("    Changes:  +%s lines\n", formatCompactNumber(agent.Metrics.LinesChanged)))
		content.WriteString(fmt.Sprintf("    Messages: %d\n", agent.Metrics.MessageCount))
		content.WriteString(fmt.Sprintf("    Duration: %s\n", formatDuration(time.Duration(agent.Metrics.DurationMs)*time.Millisecond)))
	}

	// Prompt/Task
	if agent.Prompt != "" {
		content.WriteString("\n")
		content.WriteString(tui.StyleMuted.Render("  Task:\n"))
		lines := wrapText(agent.Prompt, m.width-6)
		for _, line := range lines {
			content.WriteString("    " + line + "\n")
		}
	}

	// Pin footer to bottom
	footer := tui.StyleHelp.Render("  [L]ogs [a]ttach [e]nvim [s]hell [x]kill │ Esc to close")
	return layout.PinFooterToBottom(content.String(), footer, m.height)
}

func (m Model) renderLogs() string {
	var content strings.Builder

	// Header with follow indicator
	icon := tui.Logo()
	content.WriteString(icon + " " + tui.StyleLogo.Render("Agent Logs"))
	content.WriteString(tui.StyleMuted.Render(" │ " + m.logsAgentID[:8] + "..."))
	if m.logsFollow {
		content.WriteString("  " + tui.StyleSuccessMsg.Render("[FOLLOW]"))
	}
	content.WriteString("\n")
	content.WriteString(tui.Divider(m.width))
	content.WriteString("\n")

	viewportHeight := m.logsViewportHeight()

	if len(m.logs) == 0 {
		content.WriteString(tui.StyleEmptyState.Render("  No events recorded."))
		content.WriteString("\n")
	} else {
		// Logs come in DESC order (newest first), reverse to chronological
		// Then apply scroll offset to show a window of entries
		totalLogs := len(m.logs)

		// Calculate visible range based on scroll
		// logsScroll=0 means show oldest logs, logsScroll=max means show newest
		endIdx := totalLogs - m.logsScroll
		startIdx := max(0, endIdx-viewportHeight)

		// Render in chronological order (oldest to newest within the window)
		for i := endIdx - 1; i >= startIdx; i-- {
			e := m.logs[i]
			ts, _ := time.Parse(time.RFC3339, e.Timestamp)

			// Event type styling
			typeStyle := tui.StyleMuted
			switch e.EventType {
			case "spawned", "spawn_command":
				typeStyle = tui.StatusStyle("running")
			case "assistant":
				typeStyle = tui.StyleAccent
			case "output", "text":
				typeStyle = tui.StyleAccent
			case "error", "crashed", "spawn_failed", "stderr":
				typeStyle = tui.StatusStyle("crashed")
			case "completed", "result":
				typeStyle = tui.StatusStyle("completed")
			case "tool_use":
				typeStyle = tui.StatusStyle("executing")
			case "tool_result":
				typeStyle = tui.StyleMuted
			}

			content.WriteString("  ")
			content.WriteString(tui.StyleMuted.Render(ts.Format("15:04:05")))
			content.WriteString("  ")
			content.WriteString(typeStyle.Render(fmt.Sprintf("%-12s", e.EventType)))
			content.WriteString(" ")

			// Parse and format payload (full content, with indentation)
			display := formatLogPayload(e.EventType, e.Payload, 25)
			content.WriteString(display)
			content.WriteString("\n")
		}

		// Scroll indicator
		if totalLogs > viewportHeight {
			position := fmt.Sprintf(" [%d-%d of %d]", startIdx+1, endIdx, totalLogs)
			content.WriteString(tui.StyleMuted.Render(position))
			content.WriteString("\n")
		}
	}

	// Fixed footer with vi-style help, left-aligned
	helpText := "j/k:scroll  g/G:top/bottom  ^d/^u:page  f:follow  r:refresh  q:close"
	footer := "  " + tui.StyleHelp.Render(helpText)
	return layout.PinFooterToBottom(content.String(), footer, m.height)
}

func (m Model) renderPlanView() string {
	var content strings.Builder

	// Header with status badge
	icon := tui.Logo()
	content.WriteString(icon + " " + tui.StyleLogo.Render("Implementation Plan"))

	// Status badge
	var statusStyle lipgloss.Style
	switch m.planStatus {
	case "pending":
		statusStyle = tui.StatusStyle("spawning")
	case "draft":
		statusStyle = tui.StatusStyle("pending")
	case "approved":
		statusStyle = tui.StatusStyle("completed")
	case "executing":
		statusStyle = tui.StatusStyle("running")
	case "completed":
		statusStyle = tui.StatusStyle("completed")
	default:
		statusStyle = tui.StyleMuted
	}
	content.WriteString("  " + statusStyle.Render("["+strings.ToUpper(m.planStatus)+"]"))

	// Show planner status when pending
	if m.planStatus == "pending" && m.planPlannerStatus != "" {
		plannerStyle := tui.StatusStyle(m.planPlannerStatus)
		content.WriteString("  " + plannerStyle.Render("planner:"+m.planPlannerStatus))
	}

	content.WriteString("\n")
	content.WriteString(tui.Divider(m.width))
	content.WriteString("\n")

	viewportHeight := m.planViewportHeight()

	if m.planRendered == "" {
		content.WriteString(tui.StyleEmptyState.Render("  No plan content."))
		content.WriteString("\n")
	} else {
		// Split rendered content into lines
		lines := strings.Split(m.planRendered, "\n")
		totalLines := len(lines)

		// Apply scroll offset
		endIdx := min(m.planScroll+viewportHeight, totalLines)
		startIdx := m.planScroll

		for i := startIdx; i < endIdx; i++ {
			content.WriteString(lines[i])
			content.WriteString("\n")
		}

		// Scroll indicator
		if totalLines > viewportHeight {
			position := fmt.Sprintf(" [%d-%d of %d lines]", startIdx+1, endIdx, totalLines)
			content.WriteString(tui.StyleMuted.Render(position))
			content.WriteString("\n")
		}
	}

	// Footer with available actions based on plan state
	var helpText string
	switch m.planStatus {
	case "pending":
		helpText = "r:refresh  L:logs  q:close"
	case "draft":
		helpText = "j/k:scroll  g/G:top/bottom  a:approve  r:refresh  q:close"
	case "approved":
		helpText = "j/k:scroll  g/G:top/bottom  x:execute  r:refresh  q:close"
	default:
		helpText = "j/k:scroll  g/G:top/bottom  r:refresh  q:close"
	}
	footer := "  " + tui.StyleHelp.Render(helpText)
	return layout.PinFooterToBottom(content.String(), footer, m.height)
}

func (m Model) renderWorktreeWizard() string {
	var content strings.Builder

	// Header
	icon := tui.Logo()
	content.WriteString(icon + " " + tui.StyleLogo.Render("New Worktree"))
	content.WriteString("\n")
	content.WriteString(tui.Divider(m.width))
	content.WriteString("\n\n")

	// Step indicator
	steps := []string{"Ticket", "Description", "Project"}
	var stepIndicator []string
	for i, step := range steps {
		if i < m.worktreeStep {
			stepIndicator = append(stepIndicator, tui.StyleSuccessMsg.Render("✓ "+step))
		} else if i == m.worktreeStep {
			stepIndicator = append(stepIndicator, tui.StyleAccent.Render("→ "+step))
		} else {
			stepIndicator = append(stepIndicator, tui.StyleMuted.Render("○ "+step))
		}
	}
	content.WriteString("  " + strings.Join(stepIndicator, "  "))
	content.WriteString("\n\n")

	// Content based on step
	switch m.worktreeStep {
	case 0: // Ticket ID
		content.WriteString(tui.StyleMuted.Render("  Enter a ticket ID (e.g., ENG-123) or press Tab/Enter to skip:"))
		content.WriteString("\n\n")
		content.WriteString("  ")
		content.WriteString(m.textInput.View())
		content.WriteString("\n")

	case 1: // Description
		if m.worktreeTicketID != "" {
			content.WriteString(tui.StyleMuted.Render("  Ticket: "))
			content.WriteString(tui.StyleAccent.Render(m.worktreeTicketID))
			content.WriteString("\n\n")
		}
		content.WriteString(tui.StyleMuted.Render("  Enter a brief description for this worktree:"))
		content.WriteString("\n\n")
		content.WriteString("  ")
		content.WriteString(m.textInput.View())
		content.WriteString("\n")

	case 2: // Project selection
		if m.worktreeTicketID != "" {
			content.WriteString(tui.StyleMuted.Render("  Ticket: "))
			content.WriteString(tui.StyleAccent.Render(m.worktreeTicketID))
			content.WriteString("\n")
		}
		if m.worktreeDesc != "" {
			content.WriteString(tui.StyleMuted.Render("  Description: "))
			content.WriteString(m.worktreeDesc)
			content.WriteString("\n")
		}
		content.WriteString("\n")
		content.WriteString(tui.StyleMuted.Render("  Select project (j/k to move, Enter to confirm):"))
		content.WriteString("\n\n")

		for i, project := range m.projects {
			if i == m.worktreeProjectIdx {
				content.WriteString(tui.StyleSelectedIndicator.Render("  → "))
				content.WriteString(tui.StyleSelected.Render(project))
			} else {
				content.WriteString("    ")
				content.WriteString(project)
			}
			content.WriteString("\n")
		}
	}

	// Pin footer to bottom
	footer := tui.StyleHelp.Render("  Enter to continue · Esc to cancel")
	return layout.PinFooterToBottom(content.String(), footer, m.height)
}

func (m Model) renderPromoteWizard() string {
	var content strings.Builder

	// Header
	icon := tui.Logo()
	content.WriteString(icon + " " + tui.StyleLogo.Render("Promote to Feature"))
	content.WriteString("\n")
	content.WriteString(tui.Divider(m.width))
	content.WriteString("\n\n")

	// Show note content
	content.WriteString(tui.StyleMuted.Render("  Note:"))
	content.WriteString("\n")
	noteLines := wrapText(m.promoteNoteText, m.width-6)
	for _, line := range noteLines {
		content.WriteString("    " + line + "\n")
	}
	content.WriteString("\n")

	// Project selection
	content.WriteString(tui.StyleMuted.Render("  Select project (j/k to move, Enter to confirm):"))
	content.WriteString("\n\n")

	for i, project := range m.projects {
		if i == m.promoteProjectIdx {
			content.WriteString(tui.StyleSelectedIndicator.Render("  → "))
			content.WriteString(tui.StyleSelected.Render(project))
		} else {
			content.WriteString("    ")
			content.WriteString(project)
		}
		content.WriteString("\n")
	}

	// Pin footer to bottom
	footer := tui.StyleHelp.Render("  Enter to promote · Esc to cancel")
	return layout.PinFooterToBottom(content.String(), footer, m.height)
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

// applyInputOverlay renders the input panel as a bottom overlay
func (m Model) applyInputOverlay(content string) string {
	// Build the input panel (divider + input content)
	var inputPanel strings.Builder
	inputPanel.WriteString(tui.Divider(m.width))
	inputPanel.WriteString("\n")
	inputPanel.WriteString(m.renderInputPanel())

	// Use PinFooterToBottom to handle the layout
	return layout.PinFooterToBottom(content, inputPanel.String(), m.height)
}

// renderInputPanel renders just the input panel content (clean, full-width like Claude Code)
func (m Model) renderInputPanel() string {
	var b strings.Builder

	// Simple prompt indicator
	prompt := "> "
	if m.noteMode {
		prompt = "Note> "
	} else if m.questionMode {
		prompt = "?> "
	}

	b.WriteString(tui.StyleMuted.Render(prompt))
	b.WriteString(m.textInput.View())
	b.WriteString("\n")
	b.WriteString(tui.StyleMuted.Render("Enter to send · Alt+Enter or Ctrl+J for newline · Esc to cancel"))

	return b.String()
}

func (m Model) renderDetailFooter() string {
	// Show status message if recent
	if m.statusMsg != "" && time.Since(m.statusMsgTime) < 2*time.Second {
		return tui.StyleStatusMsg.Render(m.statusMsg)
	}

	// Build context-aware help
	help := FormatHelp(m.tab, m.level)

	update := fmt.Sprintf("%s ago", formatDuration(time.Since(m.lastUpdate)))

	padding := m.width - lipgloss.Width(help) - lipgloss.Width(update) - 2
	if padding < 1 {
		padding = 1
	}

	return tui.StyleHelp.Render(help) + strings.Repeat(" ", padding) + tui.StyleStatus.Render(update)
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

	switch m.tab {
	case TabWorktrees:
		if m.selected < len(wts) {
			wt := wts[m.selected]
			if wt.AgentID != "" {
				m.term.AttachToAgent(wt.Path, wt.AgentID)
			}
		}
	case TabAgents:
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

	switch m.tab {
	case TabWorktrees:
		if m.selected < len(wts) {
			m.term.OpenNvim(wts[m.selected].Path, true)
		}
	case TabAgents:
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
	switch m.tab {
	case TabWorktrees:
		if m.selected < len(wts) {
			agentID = wts[m.selected].AgentID
		}
	case TabAgents:
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
	switch m.tab {
	case TabWorktrees:
		if m.selected < len(wts) {
			agentID = wts[m.selected].AgentID
		}
	case TabAgents:
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
	switch m.tab {
	case TabWorktrees:
		if m.selected < len(wts) {
			agentID = wts[m.selected].AgentID
		}
	case TabAgents:
		if m.selected < len(agents) {
			agentID = agents[m.selected].ID
		}
	}

	if agentID != "" {
		return m.killAgent(agentID)
	}
	return nil
}

func (m Model) doPublishPR(wt *control.WorktreeInfo) tea.Cmd {
	return func() tea.Msg {
		result, err := m.client.PublishPR(wt.Path)
		if err != nil {
			return errMsg(err)
		}
		return publishResultMsg{path: wt.Path, prURL: result.PRURL, branch: result.Branch}
	}
}

func (m Model) doMergeLocal(wt *control.WorktreeInfo) tea.Cmd {
	return func() tea.Msg {
		result, err := m.client.MergeLocal(wt.Path)
		if err != nil {
			return errMsg(err)
		}
		return mergeResultMsg{
			path:         wt.Path,
			hasConflicts: result.HasConflicts,
			agentSpawned: result.AgentSpawned,
			message:      result.Message,
		}
	}
}

func (m Model) doCleanup(wt *control.WorktreeInfo) tea.Cmd {
	return func() tea.Msg {
		err := m.client.CleanupWorktree(wt.Path, true) // Delete branch too
		if err != nil {
			return errMsg(err)
		}
		return cleanupResultMsg{path: wt.Path}
	}
}

func (m Model) doPlanView() tea.Cmd {
	var wts []*control.WorktreeInfo
	var agents []*control.AgentInfo

	if m.level == LevelDashboard {
		wts = m.worktrees
		agents = m.agents
	} else {
		wts = m.projectWorktrees()
		agents = m.projectAgents()
	}

	var worktreePath string
	switch m.tab {
	case TabWorktrees:
		if m.selected < len(wts) {
			worktreePath = wts[m.selected].Path
		}
	case TabAgents:
		if m.selected < len(agents) {
			worktreePath = agents[m.selected].WorktreePath
		}
	}

	if worktreePath != "" {
		return m.fetchPlan(worktreePath, false)
	}
	return nil
}

func (m Model) fetchPlan(worktreePath string, forceRefresh bool) tea.Cmd {
	return func() tea.Msg {
		plan, err := m.client.GetPlan(worktreePath, forceRefresh)
		if err != nil {
			return errMsg(fmt.Errorf("failed to get plan: %w", err))
		}
		return planResultMsg{
			worktreePath:  worktreePath,
			content:       plan.Content,
			status:        plan.Status,
			agentID:       plan.AgentID,
			plannerStatus: plan.PlannerStatus,
		}
	}
}

// renderPendingPlan renders a message showing the planner is still working.
func (m Model) renderPendingPlan(plannerStatus string) string {
	var b strings.Builder

	b.WriteString("## 🔄 Plan In Progress\n\n")
	b.WriteString("The planner agent is analyzing the codebase and creating an implementation plan.\n\n")

	b.WriteString("**Planner Status:** ")
	switch plannerStatus {
	case "planning":
		b.WriteString("`thinking` - Analyzing the codebase\n")
	case "executing":
		b.WriteString("`working` - Writing the plan\n")
	case "running":
		b.WriteString("`running` - Getting started\n")
	case "completed":
		b.WriteString("`done` - Completed (refresh to see plan)\n")
	case "crashed":
		b.WriteString("`crashed` - Something went wrong\n")
	default:
		b.WriteString(fmt.Sprintf("`%s`\n", plannerStatus))
	}

	b.WriteString("\n---\n\n")
	b.WriteString("Press `r` to refresh and check for the plan.\n")
	b.WriteString("Press `L` to view the planner's logs.\n")
	b.WriteString("Press `q` to close.\n")

	return b.String()
}

func (m Model) approvePlan(worktreePath string) tea.Cmd {
	return func() tea.Msg {
		if err := m.client.ApprovePlan(worktreePath); err != nil {
			return errMsg(fmt.Errorf("failed to approve plan: %w", err))
		}
		// Refresh the plan to get updated status
		plan, err := m.client.GetPlan(worktreePath, false)
		if err != nil {
			return errMsg(err)
		}
		return planResultMsg{
			worktreePath:  worktreePath,
			content:       plan.Content,
			status:        plan.Status,
			agentID:       plan.AgentID,
			plannerStatus: plan.PlannerStatus,
		}
	}
}

func (m Model) spawnExecutor(worktreePath string) tea.Cmd {
	return func() tea.Msg {
		_, err := m.client.SpawnExecutor(worktreePath)
		if err != nil {
			return errMsg(fmt.Errorf("failed to spawn executor: %w", err))
		}
		return dataUpdateMsg{}
	}
}

func (m Model) doRetry() tea.Cmd {
	var agents []*control.AgentInfo

	if m.level == LevelDashboard {
		agents = m.agents
	} else {
		agents = m.projectAgents()
	}

	if m.tab != TabAgents || m.selected >= len(agents) {
		return m.showStatus("Select a crashed agent to retry")
	}

	agent := agents[m.selected]
	if agent.Status != "crashed" {
		return m.showStatus("Only crashed agents can be retried")
	}

	return m.respawnAgent(agent)
}

func (m Model) respawnAgent(agent *control.AgentInfo) tea.Cmd {
	return func() tea.Msg {
		// First kill the crashed agent to clean up
		_ = m.client.KillAgent(agent.ID)

		// Now spawn a new agent with the same config
		_, err := m.client.SpawnAgent(control.SpawnAgentRequest{
			WorktreePath: agent.WorktreePath,
			Archetype:    agent.Archetype,
			Prompt:       agent.Prompt,
		})
		if err != nil {
			return errMsg(fmt.Errorf("failed to respawn agent: %w", err))
		}
		return dataUpdateMsg{}
	}
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
		event, ok := <-m.client.Events()
		if !ok {
			return nil
		}
		return eventMsg(event)
	}
}

func (m Model) resolveJobProject() string {
	if m.level == LevelProject && m.selectedProject != "" {
		return m.selectedProject
	}

	switch m.tab {
	case TabWorktrees:
		if m.selected < len(m.worktrees) {
			return m.worktrees[m.selected].Project
		}
	case TabAgents:
		if m.selected < len(m.agents) {
			return m.agents[m.selected].Project
		}
	case TabJobs:
		if m.selected < len(m.jobs) {
			return m.jobs[m.selected].Project
		}
	}

	return ""
}

func (m Model) createJob(input string, isQuestion bool, project string) tea.Cmd {
	return func() tea.Msg {
		jobType := "feature"
		if isQuestion {
			jobType = "question"
		}
		_, err := m.client.CreateJob(control.CreateJobRequest{
			Input:   input,
			Project: project,
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

// formatLogPayload extracts and formats meaningful content from log payloads.
// Returns full content without truncation, with proper indentation for multi-line content.
func formatLogPayload(eventType, payload string, indentWidth int) string {
	// Try to parse as JSON and extract meaningful content
	var data map[string]any
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		// Not JSON, return raw payload with indentation
		return indentMultiline(payload, indentWidth)
	}

	var display string
	switch eventType {
	case "assistant":
		if subtype, ok := data["subtype"].(string); ok {
			if content, ok := data["content"].(string); ok {
				display = fmt.Sprintf("[%s] %s", subtype, content)
			} else {
				display = fmt.Sprintf("[%s]", subtype)
			}
		}
	case "tool_use":
		if name, ok := data["name"].(string); ok {
			if input, ok := data["input"].(string); ok {
				display = fmt.Sprintf("%s: %s", name, input)
			} else {
				display = name
			}
		}
	case "tool_result":
		if content, ok := data["content"].(string); ok {
			display = content
		}
	case "stderr":
		if line, ok := data["line"].(string); ok {
			display = line
		}
	case "spawn_command":
		if cmd, ok := data["command"].(string); ok {
			display = cmd
		}
	case "spawn_failed", "error":
		if msg, ok := data["message"].(string); ok {
			display = msg
		}
	case "result":
		if subtype, ok := data["subtype"].(string); ok {
			display = subtype
		}
	default:
		display = payload
	}

	if display == "" {
		display = payload
	}

	return indentMultiline(display, indentWidth)
}

// indentMultiline adds indentation to continuation lines for proper alignment.
func indentMultiline(s string, indentWidth int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= 1 {
		return s
	}

	indent := strings.Repeat(" ", indentWidth)
	var result strings.Builder
	result.WriteString(lines[0])
	for _, line := range lines[1:] {
		result.WriteString("\n")
		result.WriteString(indent)
		result.WriteString(line)
	}
	return result.String()
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

// formatCompactNumber formats a number with k/M suffix for readability.
func formatCompactNumber(n int) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
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
