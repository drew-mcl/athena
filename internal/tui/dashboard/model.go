// Package dashboard provides the main TUI dashboard view.
package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/drewfead/athena/internal/config"
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
	TabAdmin                // System administration and monitoring
)

const (
	inputHeightMin = 1
	inputHeightMax = 3
	ticketLabel    = "  Ticket: "
)

// Dashboard level tabs (global view)
var dashboardTabs = []Tab{TabProjects, TabWorktrees, TabJobs, TabTasks, TabQuestions, TabAdmin}

// Project level tabs (drill-in view)
var projectTabs = []Tab{TabWorktrees, TabAgents, TabTasks, TabNotes}

var promoteProviderIDs = []string{"claude", "gemini", "codex"}
var promoteProviderLabels = []string{"Claude Code (Opus)", "Gemini Auto", "Codex 5.2"}

// Model is the main dashboard model
type Model struct {
	// Data
	projects  []string // Unique project names
	worktrees []*control.WorktreeInfo
	jobs      []*control.JobInfo
	agents    []*control.AgentInfo
	notes     []*control.NoteInfo      // Quick notes/ideas
	changelog []*control.ChangelogInfo // Completed work history

	// Claude Code Tasks
	taskLists        []*control.TaskListInfo
	claudeTasks      []*control.TaskInfo
	selectedTaskList string // ID of selected task list

	// Navigation
	level           Level
	selectedProject string
	tab             Tab
	selected        int

	// Scrolling
	scrollOffset map[Tab]int

	// UI state
	width          int // Inner content width
	height         int // Inner content height
	termWidth      int // Full terminal width
	termHeight     int // Full terminal height
	inputMode      bool
	questionMode   bool // true = question, false = job
	noteMode       bool // true = adding note
	detailMode     bool // showing detail view
	detailScroll   int  // scroll offset for detail view
	detailJob      *control.JobInfo
	detailAgent    *control.AgentInfo
	detailRenderedPrompt string // cached rendered prompt
	detailRenderedPlan   string // cached rendered plan for detail view
	detailWorktree *control.WorktreeInfo // showing worktree detail
	logsMode       bool                  // showing agent logs
	logsAgentID    string
	logs           []*control.AgentEventInfo
	logsScroll     int  // scroll offset for logs viewport
	logsFollow     bool // auto-scroll to bottom
	textInput      textarea.Model
	spinner        spinner.Model
	lastUpdate     time.Time
	err            error

	// Worktree creation wizard
	worktreeMode       bool                // true = creating worktree
	worktreeStep       int                 // 0=ticket, 1=description, 2=project
	worktreeTicketID   string              // ticket ID from step 0
	worktreeDesc       string              // description from step 1
	worktreeProjectIdx int                 // selected project index in step 2
	worktreeWorkflow   config.WorkflowMode // selected workflow for this worktree (step 3)

	// Note promotion wizard
	promoteMode       bool   // true = promoting note to feature
	promoteNoteID     string // ID of note being promoted
	promoteNoteText   string // content of note being promoted
	promoteProjectIdx int    // selected project index
	promoteStep       int    // 0=Project, 1=Agent
	promoteAgentIdx   int    // selected agent provider index

	// Plan viewer mode
	planMode          bool   // true = viewing plan
	planContent       string // raw markdown content
	planRendered      string // glamour-rendered output
	planStatus        string // pending | draft | approved | executing | completed
	planPlannerStatus string // Status of planner agent (for visibility when pending)
	planScroll        int    // scroll offset
	planAgentID       string // planner agent ID for parent link
	planWorktreePath  string // worktree path for the plan

	// Context viewer mode (blackboard + state)
	contextMode         bool                           // true = viewing context
	contextWorktreePath string                         // worktree path for context
	contextProjectName  string                         // project name for state lookup
	contextBlackboard   []*control.BlackboardEntryInfo // blackboard entries
	contextSummary      *control.BlackboardSummaryInfo // blackboard summary
	contextPreview      string                         // formatted context preview
	contextScroll       int                            // scroll offset

	// Status message feedback
	statusMsg     string
	statusMsgTime time.Time

	// Layout tables (cached, updated on resize)
	worktreeTable *layout.Table
	jobTable      *layout.Table
	agentTable    *layout.Table
	taskTable     *layout.Table

	// Client connection
	client *control.Client

	// Terminal integration
	term terminal.Terminal

	// Configuration
	workflowMode config.WorkflowMode

	// Debug options
	debugKeys bool

	// Initial navigation
	initialProject string // Project to navigate to on startup
}

// Messages
type (
	tickMsg            time.Time
	dataUpdateMsg      struct{}
	errMsg             error
	eventMsg           control.Event
	fetchDataResultMsg struct {
		worktrees   []*control.WorktreeInfo
		agents      []*control.AgentInfo
		jobs        []*control.JobInfo
		notes       []*control.NoteInfo
		changelog   []*control.ChangelogInfo
		taskLists   []*control.TaskListInfo
		claudeTasks []*control.TaskInfo
		err         error
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
	abandonResultMsg struct {
		path string
	}
	contextResultMsg struct {
		worktreePath string
		projectName  string
		blackboard   []*control.BlackboardEntryInfo
		summary      *control.BlackboardSummaryInfo
		preview      string
	}
	markdownRenderedMsg struct {
		target  string // "plan" or "agent:<id>"
		content string
	}
	detailPlanResultMsg struct {
		agentID string
		content string
	}
	clearStatusMsg struct{}
	clearErrMsg    struct{} // Auto-clears error after timeout
)

// New creates a new dashboard model
func New(client *control.Client, cfg *config.Config) Model {
	ti := textarea.New()
	ti.Placeholder = "Type here..."
	ti.Prompt = "" // No prompt in the textarea itself (we render it outside)
	ti.CharLimit = 2000
	ti.SetWidth(80)              // Will be updated on resize
	ti.SetHeight(inputHeightMin) // Start with a single line
	ti.ShowLineNumbers = false
	ti.FocusedStyle.CursorLine = lipgloss.NewStyle() // No highlight on current line
	ti.FocusedStyle.Base = lipgloss.NewStyle()       // No border/background
	ti.BlurredStyle.Base = lipgloss.NewStyle()       // No border/background when blurred

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(tui.ColorAccent)

	// Create responsive tables with generous max widths for wide terminals
	worktreeTable := layout.NewTable([]layout.Column{
		{Header: "BRANCH", MinWidth: 15, MaxWidth: 30, Flex: 1},
		{Header: "SUMMARY", MinWidth: 30, MaxWidth: 0, Flex: 4}, // Largest column
		{Header: "AGENTS", MinWidth: 6, MaxWidth: 8, Flex: 0},   // Agent count
		{Header: "STATUS", MinWidth: 14, MaxWidth: 14, Flex: 0}, // Fixed width status
		{Header: "GIT", MinWidth: 8, MaxWidth: 12, Flex: 0},
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
		{Header: "TYPE", MinWidth: 8, MaxWidth: 10, Flex: 0},
		{Header: "WORKTREE", MinWidth: 12, MaxWidth: 18, Flex: 1},
		{Header: "ACTIVITY", MinWidth: 25, MaxWidth: 45, Flex: 2},
		{Header: "TOKENS", MinWidth: 6, MaxWidth: 8, Flex: 0},
		{Header: "CACHE", MinWidth: 6, MaxWidth: 8, Flex: 0},
		{Header: "TOOLS", MinWidth: 5, MaxWidth: 6, Flex: 0},
		{Header: "AGE", MinWidth: 4, MaxWidth: 6, Flex: 0},
	})

	taskTable := layout.NewTable([]layout.Column{
		{Header: "ST", MinWidth: 2, MaxWidth: 2, Flex: 0},
		{Header: "TASK", MinWidth: 30, MaxWidth: 0, Flex: 4},
		{Header: "LIST", MinWidth: 12, MaxWidth: 20, Flex: 1},
		{Header: "OWNER", MinWidth: 10, MaxWidth: 15, Flex: 0},
		{Header: "AGE", MinWidth: 5, MaxWidth: 8, Flex: 0},
	})

	// Default workflow mode if config not provided
	workflowMode := config.WorkflowModeApprove
	if cfg != nil {
		workflowMode = cfg.UI.WorkflowMode
	}

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
		taskTable:     taskTable,
		workflowMode:  workflowMode,
	}
}

// WithDebugKeys enables or disables key logging for the TUI.
func (m Model) WithDebugKeys(enabled bool) Model {
	m.debugKeys = enabled
	return m
}

// WithInitialProject sets a project to navigate to on startup.
func (m Model) WithInitialProject(project string) Model {
	m.initialProject = project
	return m
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
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Global keybindings
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

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
		if m.contextMode {
			return m.handleContextMode(msg)
		}
		if m.logsMode {
			return m.handleLogsMode(msg)
		}
		if m.detailMode {
			return m.handleDetailMode(msg)
		}
		return m.handleNormalMode(msg)

	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)

	case tickMsg:
		return m, tea.Batch(m.fetchData, m.tick())

	case fetchDataResultMsg:
		return m.handleFetchDataResult(msg)

	case errMsg:
		return m.handleErr(msg)

	case clearErrMsg:
		m.err = nil
		return m, nil

	case dataUpdateMsg:
		return m, m.fetchData

	case eventMsg:
		return m.handleControlEvent(msg)

	case logsResultMsg:
		return m.handleLogsResult(msg)

	case planResultMsg:
		return m.handlePlanResult(msg)

	case markdownRenderedMsg:
		return m.handleMarkdownRendered(msg)

	case detailPlanResultMsg:
		return m.handleDetailPlanResult(msg)

	case contextResultMsg:
		return m.handleContextResult(msg)

	case publishResultMsg:
		return m.handlePublishResult(msg)

	case mergeResultMsg:
		return m.handleMergeResult(msg)

	case cleanupResultMsg:
		return m.handleCleanupResult(msg)

	case clearStatusMsg:
		m.statusMsg = ""
		return m, nil

	case spinner.TickMsg:
		return m.handleSpinnerTick(msg)
	}

	return m, nil
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

func (m *Model) ensureDashboardTab() {
	if !containsTab(dashboardTabs, m.tab) {
		m.tab = TabWorktrees
	}
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
			m.ensureDashboardTab()
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
			m.ensureDashboardTab()
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
				if m.selected < len(m.agents) {
					// Show agent detail at dashboard level
					return m.viewAgent(m.agents[m.selected])
				}
			case TabQuestions:
				// Show question detail (questions are jobs with type "question")
				questions := m.questions()
				if m.selected < len(questions) {
					m.detailJob = questions[m.selected]
					m.detailMode = true
					return m, nil
				}
			case TabAgents, TabAdmin:
				if m.selected < len(m.agents) {
					// Show agent detail at dashboard level
					return m.viewAgent(m.agents[m.selected])
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
			case TabWorktrees:
				wts := m.projectWorktrees()
				if m.selected < len(wts) {
					m.detailWorktree = wts[m.selected]
					m.detailMode = true
					return m, nil
				}
			case TabAgents:
				agents := m.projectAgents()
				if m.selected < len(agents) {
					return m.viewAgent(agents[m.selected])
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
			m.worktreeWorkflow = m.workflowMode // Default to global setting
			m.textInput.Placeholder = "Ticket ID (or leave blank)..."
			m.resetTextInput()
			m.textInput.Focus()
			return m, textarea.Blink
		}
		// New note on notes tab (at any level)
		if m.tab == TabNotes {
			m.inputMode = true
			m.noteMode = true
			m.questionMode = false
			m.textInput.Placeholder = "Enter note..."
			m.resetTextInput()
			m.textInput.Focus()
			return m, textarea.Blink
		}
		// New job/question at any level (except notes tab)
		m.inputMode = true
		m.questionMode = false
		m.noteMode = false
		m.textInput.Placeholder = "Enter task description..."
		m.resetTextInput()
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

	case "w":
		// Cycle workflow mode: automatic → approve → manual → automatic
		m.workflowMode = m.workflowMode.CycleWorkflowMode()
		return m, m.showStatus(fmt.Sprintf("Workflow mode: %s", m.workflowMode))

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

	case "C":
		// View agent context (blackboard + state)
		if !IsActionAvailable("C", m.tab, m.level) {
			return m, m.showStatus(GetActionTooltip("C", m.tab, m.level))
		}
		return m, m.doContextView()

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

	case "A":
		// Quick approve plan directly from agents tab
		if m.tab != TabAgents {
			return m, m.showStatus("Approve only available on agents tab")
		}
		return m, m.doQuickApprove()

	case "X":
		// Quick execute plan directly from agents tab
		if m.tab != TabAgents {
			return m, m.showStatus("Execute only available on agents tab")
		}
		return m, m.doQuickExecute()

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
				m.promoteAgentIdx = 0 // Default to first agent

				// Smart step selection
				if m.level == LevelProject && m.selectedProject != "" {
					// Already in a project, skip step 0
					m.promoteStep = 1
					// Find project index
					for i, p := range m.projects {
						if p == m.selectedProject {
							m.promoteProjectIdx = i
							break
						}
					}
				} else {
					// Dashboard level, ask for project
					m.promoteStep = 0
					m.promoteProjectIdx = 0
				}

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

	case "D":
		// Abandon worktree (worktrees tab only)
		if m.tab != TabWorktrees {
			return m, m.showStatus("Abandon only available on worktrees tab")
		}
		wt := m.getSelectedWorktree()
		if wt == nil {
			return m, m.showStatus("No worktree selected")
		}
		if wt.IsMain {
			return m, m.showStatus("Cannot abandon main worktree")
		}
		return m, m.doAbandon(wt)
	}

	return m, nil
}

func (m Model) handleInputMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.inputMode = false
		m.noteMode = false
		m.resetTextInput() // Calls syncInputHeight
		return m, nil

	case "shift+enter":
		m.textInput.InsertString("\n")
		m.syncInputHeight()
		return m, nil

	case "enter":
		// Plain Enter submits
		input := strings.TrimSpace(m.textInput.Value())
		if input != "" {
			if m.noteMode {
				// Add note via API
				m.inputMode = false
				m.noteMode = false
				m.resetTextInput() // Calls syncInputHeight
				return m, m.createNote(input)
			}
			isQuestion := m.questionMode
			project := m.resolveJobProject()
			if project == "" {
				m.inputMode = false
				m.questionMode = false
				m.resetTextInput() // Calls syncInputHeight
				return m, m.showStatus("Select a project before creating a job")
			}
			m.inputMode = false
			m.questionMode = false
			m.resetTextInput() // Calls syncInputHeight
			return m, m.createJob(input, isQuestion, project)
		}
		return m, nil

	case "ctrl+j":
		m.textInput.InsertString("\n")
		m.syncInputHeight()
		return m, nil
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	m.syncInputHeight()
	return m, cmd
}

func (m Model) handleDetailMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if shouldExitDetail(key) {
		return m.exitDetailMode(), nil
	}

	if updated, handled := m.handleDetailScroll(key); handled {
		return updated, nil
	}

	return m.handleDetailAgentAction(key)
}

func shouldExitDetail(key string) bool {
	return key == "esc" || key == "q" || key == "enter"
}

func (m Model) exitDetailMode() Model {
	m.detailMode = false
	m.detailScroll = 0
	m.detailJob = nil
	m.detailAgent = nil
	m.detailWorktree = nil
	return m
}

func (m Model) handleDetailScroll(key string) (Model, bool) {
	switch key {
	case "j", "down":
		m.detailScroll++
	case "k", "up":
		if m.detailScroll > 0 {
			m.detailScroll--
		}
	case "g":
		m.detailScroll = 0
	case "G":
		m.detailScroll = 10000 // Hack to go to bottom, renderer will clamp
	case "ctrl+d":
		m.detailScroll += m.height / 2
	case "ctrl+u":
		if m.detailScroll > m.height/2 {
			m.detailScroll -= m.height / 2
		} else {
			m.detailScroll = 0
		}
	default:
		return m, false
	}
	return m, true
}

func (m Model) handleDetailAgentAction(key string) (tea.Model, tea.Cmd) {
	if m.detailAgent == nil {
		return m, nil
	}
	switch key {
	case "L":
		return m, m.fetchLogs(m.detailAgent.ID)
	case "a":
		m.term.AttachToAgent(m.detailAgent.WorktreePath, m.detailAgent.ID)
	case "e":
		m.term.OpenNvim(m.detailAgent.WorktreePath, true)
	case "s":
		m.term.OpenShell(m.detailAgent.WorktreePath)
	case "x":
		agentID := m.detailAgent.ID
		m.detailMode = false
		m.detailAgent = nil
		return m, m.killAgent(agentID)
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
	case "esc", "q", "Q", "backspace":
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

func (m Model) handleContextMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	maxScroll := max(0, m.contextViewportLines()-m.contextViewportHeight())
	halfPage := max(1, m.contextViewportHeight()/2)

	switch msg.String() {
	case "esc", "q":
		m.contextMode = false
		m.contextBlackboard = nil
		m.contextSummary = nil
		m.contextPreview = ""
		m.contextWorktreePath = ""
		m.contextScroll = 0
		return m, nil

	case "r":
		// Refresh context
		return m, m.fetchContext(m.contextWorktreePath, m.contextProjectName)

	// Vi-style scrolling
	case "j", "down":
		m.contextScroll = min(m.contextScroll+1, maxScroll)
	case "k", "up":
		m.contextScroll = max(m.contextScroll-1, 0)
	case "g":
		m.contextScroll = 0
	case "G":
		m.contextScroll = maxScroll
	case "ctrl+d":
		m.contextScroll = min(m.contextScroll+halfPage, maxScroll)
	case "ctrl+u":
		m.contextScroll = max(m.contextScroll-halfPage, 0)
	}
	return m, nil
}

// contextViewportHeight returns available lines for context content
func (m Model) contextViewportHeight() int {
	// height - header(3) - footer(2) - padding(2)
	return max(5, m.height-7)
}

// contextViewportLines returns total lines in context preview
func (m Model) contextViewportLines() int {
	if m.contextPreview == "" {
		return 1
	}
	return strings.Count(m.contextPreview, "\n") + 1
}

func (m Model) renderContextView() string {
	var content strings.Builder

	// Header
	icon := tui.Logo()
	content.WriteString(icon + " " + tui.StyleLogo.Render("Agent Context"))
	content.WriteString("\n")
	content.WriteString(tui.Divider(m.width))
	content.WriteString("\n")

	viewportHeight := m.contextViewportHeight()

	// Summary section
	if m.contextSummary != nil {
		content.WriteString(tui.StyleAccent.Render("Summary"))
		content.WriteString(fmt.Sprintf("  Decisions: %d  Findings: %d  Attempts: %d  Questions: %d (unresolved: %d)\n\n",
			m.contextSummary.DecisionCount,
			m.contextSummary.FindingCount,
			m.contextSummary.AttemptCount,
			m.contextSummary.QuestionCount,
			m.contextSummary.UnresolvedCount))
	}

	// Preview content or empty state
	if m.contextPreview == "" && len(m.contextBlackboard) == 0 {
		content.WriteString(tui.StyleEmptyState.Render("  No context entries for this worktree yet."))
		content.WriteString("\n\n")
		content.WriteString(tui.StyleMuted.Render("  Context is populated when agents run and emit markers like:"))
		content.WriteString("\n")
		content.WriteString(tui.StyleMuted.Render("  ## DECISION: Use JWT for auth"))
		content.WriteString("\n")
		content.WriteString(tui.StyleMuted.Render("  ## FINDING: Auth code is at internal/auth/"))
		content.WriteString("\n")
	} else if m.contextPreview != "" {
		// Display the assembled context preview
		lines := strings.Split(m.contextPreview, "\n")
		totalLines := len(lines)

		// Apply scroll offset
		endIdx := min(m.contextScroll+viewportHeight, totalLines)
		startIdx := m.contextScroll

		viewportLines := lines[startIdx:endIdx]
		content.WriteString(strings.Join(viewportLines, "\n"))

		// Scroll indicator
		if totalLines > viewportHeight {
			content.WriteString("\n")
			position := fmt.Sprintf(" [%d-%d of %d lines]", startIdx+1, endIdx, totalLines)
			content.WriteString(tui.StyleMuted.Render(position))
		}
	} else {
		// No preview but we have blackboard entries - render manually
		content.WriteString(tui.StyleAccent.Render("Blackboard Entries"))
		content.WriteString("\n\n")

		// Group by type
		decisions := []*control.BlackboardEntryInfo{}
		findings := []*control.BlackboardEntryInfo{}
		attempts := []*control.BlackboardEntryInfo{}
		questions := []*control.BlackboardEntryInfo{}

		for _, e := range m.contextBlackboard {
			switch e.EntryType {
			case "decision":
				decisions = append(decisions, e)
			case "finding":
				findings = append(findings, e)
			case "attempt":
				attempts = append(attempts, e)
			case "question":
				questions = append(questions, e)
			}
		}

		if len(decisions) > 0 {
			content.WriteString(tui.StyleAccent.Render("Decisions"))
			content.WriteString("\n")
			for _, d := range decisions {
				content.WriteString(fmt.Sprintf("  • %s\n", truncateContent(d.Content, 80)))
			}
			content.WriteString("\n")
		}

		if len(findings) > 0 {
			content.WriteString(tui.StyleAccent.Render("Findings"))
			content.WriteString("\n")
			for _, f := range findings {
				content.WriteString(fmt.Sprintf("  • %s\n", truncateContent(f.Content, 80)))
			}
			content.WriteString("\n")
		}

		if len(attempts) > 0 {
			content.WriteString(tui.StyleAccent.Render("Attempts"))
			content.WriteString("\n")
			for _, a := range attempts {
				content.WriteString(fmt.Sprintf("  • %s\n", truncateContent(a.Content, 80)))
			}
			content.WriteString("\n")
		}

		if len(questions) > 0 {
			content.WriteString(tui.StyleAccent.Render("Questions"))
			content.WriteString("\n")
			for _, q := range questions {
				resolved := ""
				if q.Resolved {
					resolved = " [resolved]"
				}
				content.WriteString(fmt.Sprintf("  • %s%s\n", truncateContent(q.Content, 70), resolved))
			}
			content.WriteString("\n")
		}
	}

	// Footer
	footer := m.wrapHelpText("j/k:scroll  g/G:top/bottom  r:refresh  q:close")
	return layout.PinFooterToBottom(content.String(), footer, m.height)
}

// truncateContent truncates content to maxLen with ellipsis
func truncateContent(s string, maxLen int) string {
	// Remove newlines for display
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
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

func (m Model) renderMarkdownCmd(target, content string) tea.Cmd {
	width := m.width - 4
	if width < 40 {
		width = 40
	}
	return func() tea.Msg {
		r, err := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return markdownRenderedMsg{target: target, content: content}
		}
		out, err := r.Render(content)
		if err != nil {
			return markdownRenderedMsg{target: target, content: content}
		}
		return markdownRenderedMsg{target: target, content: out}
	}
}

func (m Model) handleWorktreeWizard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Cancel wizard
		m.worktreeMode = false
		m.resetTextInput()
		return m, nil

	case "enter":
		switch m.worktreeStep {
		case 0: // Ticket ID step
			ticketID := strings.TrimSpace(m.textInput.Value())
			m.worktreeTicketID = ticketID
			m.resetTextInput()

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
			m.resetTextInput()
			m.worktreeStep = 2
			return m, nil

		case 2: // Project selection step
			if len(m.projects) == 0 {
				return m, m.showStatus("No projects available")
			}
			// Move to workflow selection
			m.worktreeStep = 3
			return m, nil

		case 3: // Workflow selection step
			// Create the worktree with the selected workflow
			project := m.projects[m.worktreeProjectIdx]
			m.worktreeMode = false

			return m, m.createWorktreeCmd(project, m.worktreeTicketID, m.worktreeDesc, m.worktreeWorkflow)
		}

	case "j", "down":
		// In project selection, move down
		if m.worktreeStep == 2 {
			if m.worktreeProjectIdx < len(m.projects)-1 {
				m.worktreeProjectIdx++
			}
			return m, nil
		}
		// In workflow selection, cycle modes
		if m.worktreeStep == 3 {
			m.worktreeWorkflow = m.worktreeWorkflow.CycleWorkflowMode()
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
		// In workflow selection, cycle modes (backwards logic same as forward for 3 items)
		if m.worktreeStep == 3 {
			m.worktreeWorkflow = m.worktreeWorkflow.CycleWorkflowMode()
			return m, nil
		}

	case "tab":
		// Tab skips ticket entry with empty value
		if m.worktreeStep == 0 {
			m.worktreeTicketID = ""
			m.resetTextInput()
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
		m.syncInputHeight()
		return m, cmd
	}

	return m, nil
}

func (m Model) createWorktreeCmd(project, ticketID, description string, workflow config.WorkflowMode) tea.Cmd {
	return func() tea.Msg {
		mainRepoPath, err := m.resolveMainRepoPath(project)
		if err != nil {
			return errMsg(err)
		}

		_, err = m.client.CreateWorktree(control.CreateWorktreeRequest{
			MainRepoPath: mainRepoPath,
			TicketID:     ticketID,
			Description:  description,
			WorkflowMode: string(workflow),
		})
		if err != nil {
			return errMsg(err)
		}
		return dataUpdateMsg{}
	}
}

func (m Model) resolveMainRepoPath(project string) (string, error) {
	if path := m.findProjectRepo(project, true); path != "" {
		return path, nil
	}
	if path := m.findProjectRepo(project, false); path != "" {
		return path, nil
	}
	return "", fmt.Errorf("no worktree found for project %s", project)
}

func (m Model) findProjectRepo(project string, mainOnly bool) string {
	for _, wt := range m.worktrees {
		if wt.Project != project {
			continue
		}
		if mainOnly && !wt.IsMain {
			continue
		}
		return wt.Path
	}
	return ""
}

func (m Model) handlePromoteNote(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Cancel promotion
		m = m.resetPromoteState()
		return m, nil

	case "enter":
		return m.handlePromoteEnter()

	case "j", "down":
		m = m.promoteMove(1)
		return m, nil

	case "k", "up":
		m = m.promoteMove(-1)
		return m, nil

	case "backspace", "delete", "h", "left":
		// Go back a step
		if m.promoteStep > 0 {
			m.promoteStep--
			return m, nil
		}
	}

	return m, nil
}

func (m Model) resetPromoteState() Model {
	m.promoteMode = false
	m.promoteNoteID = ""
	m.promoteNoteText = ""
	m.promoteStep = 0
	return m
}

func (m Model) handlePromoteEnter() (tea.Model, tea.Cmd) {
	if m.promoteStep == 0 {
		// Project selected, move to Agent selection
		if len(m.projects) == 0 {
			return m, m.showStatus("No projects available")
		}
		m.promoteStep = 1
		return m, nil
	}
	if m.promoteStep == 1 {
		// Agent selected, execute promotion
		project := m.projects[m.promoteProjectIdx]
		provider := promoteProviderID(m.promoteAgentIdx)

		m.promoteMode = false
		return m, m.promoteNoteCmd(project, m.promoteNoteID, m.promoteNoteText, provider)
	}
	return m, nil
}

func (m Model) promoteMove(delta int) Model {
	if m.promoteStep == 0 {
		m.promoteProjectIdx = clampIndex(m.promoteProjectIdx+delta, len(m.projects))
		return m
	}
	m.promoteAgentIdx = clampIndex(m.promoteAgentIdx+delta, len(promoteProviderIDs))
	return m
}

func promoteProviderID(index int) string {
	if index < 0 || index >= len(promoteProviderIDs) {
		return promoteProviderIDs[0]
	}
	return promoteProviderIDs[index]
}

func clampIndex(index, size int) int {
	if size <= 0 {
		return 0
	}
	if index < 0 {
		return 0
	}
	if index >= size {
		return size - 1
	}
	return index
}

func (m Model) promoteNoteCmd(project, noteID, noteText, provider string) tea.Cmd {
	return func() tea.Msg {
		mainRepoPath, err := m.resolveMainRepoPath(project)
		if err != nil {
			return errMsg(err)
		}

		// Create worktree with note as description
		_, err = m.client.CreateWorktree(control.CreateWorktreeRequest{
			MainRepoPath: mainRepoPath,
			TicketID:     "", // No ticket for promoted notes
			Description:  noteText,
			WorkflowMode: string(m.workflowMode),
			Provider:     provider,
			SourceNoteID: noteID, // Track source note for abandon rollback
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
		case TabTasks:
			return max(0, len(m.claudeTasks)-1)
		case TabQuestions:
			return max(0, len(m.questions())-1)
		case TabAdmin:
			return max(0, len(m.agents)-1)
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

	if m.contextMode {
		return m.renderContextView()
	}

	content, footer := m.renderCurrentView()

	// Combine content and footer
	fullView := layout.PinFooterToBottom(content, footer, m.height)

	// Input overlay - pinned to bottom
	if m.inputMode {
		fullView = m.applyInputOverlay(fullView)
	}

	return fullView
}

func (m Model) renderCurrentView() (string, string) {
	if m.detailMode {
		return m.renderDetailOverlay()
	}
	if m.logsMode {
		return m.renderLogs()
	}
	if m.planMode {
		return m.renderPlanView()
	}
	if m.worktreeMode {
		return m.renderWorktreeWizard()
	}
	if m.promoteMode {
		return m.renderPromoteWizard()
	}
	return m.renderMainView()
}

func (m Model) renderDetailOverlay() (string, string) {
	switch {
	case m.detailJob != nil:
		return m.renderJobDetail()
	case m.detailAgent != nil:
		return m.renderAgentDetail()
	case m.detailWorktree != nil:
		return m.renderWorktreeDetail()
	default:
		return "", ""
	}
}

func (m Model) renderMainView() (string, string) {
	if m.level == LevelDashboard {
		return m.renderDashboard()
	}
	return m.renderProjectDetail()
}

func (m Model) renderDashboard() (string, string) {
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

	return content.String(), m.renderFooter()
}

func (m Model) renderDaemonStatus() string {
	if m.client != nil && m.client.Connected() {
		return tui.StyleSuccess.Render("* daemon")
	}
	return tui.StyleDanger.Render("x daemon")
}

func (m Model) renderWorkflowStatus() string {
	// Color coding: automatic=green, approve=yellow, manual=gray
	var style lipgloss.Style
	switch m.workflowMode {
	case config.WorkflowModeAutomatic:
		style = tui.StyleSuccess // Green - fully automated
	case config.WorkflowModeApprove:
		style = tui.StyleWarning // Yellow - needs approval
	case config.WorkflowModeManual:
		style = tui.StyleMuted // Gray - manual control
	default:
		style = tui.StyleMuted
	}
	return style.Render(string(m.workflowMode))
}

func (m Model) renderHeader() string {
	// Logo + title
	logo := tui.Logo()
	title := tui.StyleLogo.Render(" athena")

	// Tab names for display
	tabNames := map[Tab]string{
		TabProjects:  "overview",
		TabWorktrees: "worktrees",
		TabJobs:      "agents",
		TabQuestions: "questions",
		TabAgents:    "agents",
		TabTasks:     "tasks",
		TabNotes:     "notes",
		TabAdmin:     "admin",
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
	// Format: daemon │ workflow: mode │ stats
	stats = m.renderDaemonStatus() +
		tui.StyleMuted.Render(" │ ") +
		tui.StyleMuted.Render("workflow: ") + m.renderWorkflowStatus() +
		tui.StyleMuted.Render(" │ ") + stats

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

func (m Model) renderProjectDetail() (string, string) {
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

	return content.String(), m.renderDetailFooter()
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

	// Format: daemon │ workflow: mode │ stats
	stats := m.renderDaemonStatus() +
		tui.StyleMuted.Render(" │ ") +
		tui.StyleMuted.Render("workflow: ") + m.renderWorkflowStatus() +
		tui.StyleMuted.Render(" │ ") +
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

// applyScroll applies scroll offset to content, returning the visible window.
func (m Model) applyScroll(content, footer string) (string, string) {
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	// Calculate available height for content
	footerLines := strings.Count(footer, "\n") + 1
	contentHeight := max(1, m.height-footerLines)

	// Clamp scroll
	maxScroll := max(0, totalLines-contentHeight)
	if m.detailScroll > maxScroll {
		m.detailScroll = maxScroll
	}

	start := m.detailScroll
	end := min(start+contentHeight, totalLines)

	visibleLines := lines[start:end]

	// Add scroll indicator if needed
	if totalLines > contentHeight {
		// We might overwrite the last line or append to footer?
		// Let's append to footer for simplicity
		percent := int(float64(start) / float64(max(1, maxScroll)) * 100)
		scrollInd := fmt.Sprintf(" %d%%", percent)
		footer = strings.TrimRight(footer, " ") + tui.StyleMuted.Render(scrollInd)
	}

	return strings.Join(visibleLines, "\n"), footer
}

func (m Model) renderJobDetail() (string, string) {
	var content strings.Builder
	job := m.detailJob

	m.renderJobHeader(&content, job)
	m.renderJobTask(&content, job)
	if job.Type == "question" {
		m.renderJobAnswer(&content, job)
	} else {
		m.renderJobFeatureDetails(&content, job)
	}

	return m.applyScroll(content.String(), m.wrapHelpText("j/k:scroll  g/G:top/bottom  Esc/q:close"))
}

func (m Model) renderJobHeader(content *strings.Builder, job *control.JobInfo) {
	icon := tui.Logo()
	typeLabel := "Job"
	if job.Type == "question" {
		typeLabel = "Question"
	}
	header := icon + " " + tui.StyleLogo.Render(typeLabel)
	if job.Project != "" {
		header += tui.StyleMuted.Render(" @ ") + tui.StyleAccent.Render(job.Project)
	}
	content.WriteString(header)
	content.WriteString(tui.StyleMuted.Render(" │ " + job.Status))
	content.WriteString("\n")
	content.WriteString(tui.Divider(m.width))
	content.WriteString("\n\n")
}

func (m Model) renderJobTask(content *strings.Builder, job *control.JobInfo) {
	content.WriteString(tui.StyleMuted.Render("  Task: "))
	content.WriteString("\n")

	taskBox := tui.StyleInputBox.
		Width(m.width-6).
		Padding(0, 1).
		Render(job.NormalizedInput)

	content.WriteString("  " + taskBox)
	content.WriteString("\n\n")
}

func (m Model) renderJobAnswer(content *strings.Builder, job *control.JobInfo) {
	content.WriteString(tui.StyleMuted.Render("  Answer:\n"))
	if job.Answer != "" {
		lines := wrapText(job.Answer, m.width-4)
		for _, line := range lines {
			content.WriteString("  " + line + "\n")
		}
		return
	}
	if job.Status == "pending" || job.Status == "executing" {
		content.WriteString(tui.StyleMuted.Render("  Processing...\n"))
		return
	}
	content.WriteString(tui.StyleMuted.Render("  No answer yet.\n"))
}

func (m Model) renderJobFeatureDetails(content *strings.Builder, job *control.JobInfo) {
	if job.AgentID != "" {
		content.WriteString(tui.StyleMuted.Render("  Agent: "))
		content.WriteString(shortID(job.AgentID, 8))
		content.WriteString("\n")
	}
	if job.WorktreePath != "" {
		label := "  Worktree: "
		maxLen := max(3, m.width-len(label))
		content.WriteString(tui.StyleMuted.Render(label))
		content.WriteString(truncatePathSafe(job.WorktreePath, maxLen))
		content.WriteString("\n")
	}
}

func (m Model) renderAgentDetail() (string, string) {
	agent := m.detailAgent

	// 1. Build Header (Fixed)
	var header strings.Builder
	m.renderAgentDetailHeader(&header, agent)
	m.renderAgentDetailInfo(&header, agent)
	headerStr := header.String()

	// 2. Build Content (Scrollable)
	var content strings.Builder
	m.renderAgentSummary(&content, agent)
	m.renderAgentMetrics(&content, agent)
	m.renderAgentPlan(&content, agent)
	m.renderAgentPrompt(&content, agent)
	contentStr := content.String()

	// 3. Footer (Fixed)
	footer := tui.StyleHelp.Render("  j/k:scroll  g/G:top/bottom  [L]ogs [a]ttach [e]nvim [s]hell [x]kill  Esc/q:close")

	// 4. Calculate Viewport
	headerLines := strings.Count(headerStr, "\n")
	footerLines := strings.Count(footer, "\n") + 1

	// Available height for scrollable content
	// We subtract header and footer height from total height
	viewportHeight := max(1, m.height-headerLines-footerLines)

	// 5. Slice Content
	lines := strings.Split(contentStr, "\n")
	totalLines := len(lines)

	// Handle scrolling (clamping for display)
	maxScroll := max(0, totalLines-viewportHeight)
	scroll := m.detailScroll
	if scroll > maxScroll {
		scroll = maxScroll
	}

	start := scroll
	end := min(start+viewportHeight, totalLines)

	return m.applyScroll(content.String(), m.wrapHelpText("j/k:scroll  g/G:top/bottom  [L]ogs [a]ttach [e]nvim [s]hell [x]kill  Esc/q:close"))
}

func (m Model) renderAgentDetailHeader(content *strings.Builder, agent *control.AgentInfo) {
	icon := tui.StatusStyle(agent.Status).Render(tui.StatusIcons[agent.Status])
	content.WriteString(icon + " ")
	content.WriteString(tui.StyleLogo.Render("Agent"))
	content.WriteString(tui.StyleMuted.Render(" │ "))
	content.WriteString(tui.StatusStyle(agent.Status).Render(agent.Status))
	content.WriteString("\n")
	content.WriteString(tui.Divider(m.width))
	content.WriteString("\n\n")
}

func (m Model) renderAgentDetailInfo(content *strings.Builder, agent *control.AgentInfo) {
	content.WriteString(tui.StyleMuted.Render("  ID:        "))
	content.WriteString(agent.ID)
	content.WriteString("\n")

	content.WriteString(tui.StyleMuted.Render("  Project:   "))
	content.WriteString(agent.ProjectName)
	content.WriteString("\n")

	content.WriteString(tui.StyleMuted.Render("  Archetype: "))
	content.WriteString(agent.Archetype)
	content.WriteString("\n")

	content.WriteString(tui.StyleMuted.Render("  Harness:   "))
	content.WriteString(tui.StyleAccent.Render(agentHarnessLabel(agent)))
	content.WriteString("\n")

	label := "  Worktree:  "
	maxLen := max(3, m.width-len(label))
	content.WriteString(tui.StyleMuted.Render(label))
	content.WriteString(truncatePathSafe(agent.WorktreePath, maxLen))
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

	if agent.Archetype == "planner" {
		content.WriteString(tui.StyleMuted.Render("  Plan Status: "))
		content.WriteString(tui.StatusStyle(agent.PlanStatus).Render(agent.PlanStatus))
		content.WriteString("\n")
	}
}

func agentHarnessLabel(agent *control.AgentInfo) string {
	// Provider metadata is not exposed yet; default to Claude Code for now.
	if agent.ClaudeSessionID != "" {
		return "Claude Code (Opus)"
	}
	return "Claude Code"
}

func (m Model) renderAgentSummary(content *strings.Builder, agent *control.AgentInfo) {
	summary := ""
	for _, wt := range m.worktrees {
		if wt.Path != agent.WorktreePath {
			continue
		}
		summary = wt.Summary
		if summary == "" && wt.Description != "" {
			summary = wt.Description
		}
		break
	}
	if summary == "" {
		return
	}
	content.WriteString("\n")
	content.WriteString(tui.StyleMuted.Render("  Summary:"))
	content.WriteString("\n")
	summaryLines := wrapText(summary, m.width-6)
	for _, line := range summaryLines {
		content.WriteString("    " + line + "\n")
	}
}

func (m Model) renderAgentMetrics(content *strings.Builder, agent *control.AgentInfo) {
	content.WriteString("\n")
	content.WriteString(m.renderMetricsPanel(agent.Metrics))

	if agent.Metrics == nil {
		return
	}
	if agent.Metrics.FilesRead == 0 && agent.Metrics.FilesWritten == 0 && agent.Metrics.LinesChanged == 0 {
		return
	}
	content.WriteString("  ")
	content.WriteString(renderFileActivity(agent.Metrics))
	content.WriteString("\n")
}

func (m Model) renderAgentPrompt(content *strings.Builder, agent *control.AgentInfo) {
	if agent.Prompt == "" {
		return
	}
	content.WriteString("\n")
	content.WriteString(tui.StyleMuted.Render("  Task:\n"))

	var renderedTask string
	if m.detailAgent != nil && agent.ID == m.detailAgent.ID && m.detailRenderedPrompt != "" {
		renderedTask = m.detailRenderedPrompt
	} else {
		renderedTask = m.renderMarkdown(agent.Prompt)
	}

	for _, line := range strings.Split(renderedTask, "\n") {
		content.WriteString("    " + line + "\n")
	}
}

func (m Model) renderAgentPlan(content *strings.Builder, agent *control.AgentInfo) {
	if m.detailRenderedPlan == "" {
		return
	}
	content.WriteString("\n")
	content.WriteString(tui.StyleMuted.Render("  Implementation Plan:"))
	content.WriteString("\n")

	for _, line := range strings.Split(m.detailRenderedPlan, "\n") {
		content.WriteString("    " + line + "\n")
	}
}

func (m Model) renderWorktreeDetail() (string, string) {
	var content strings.Builder
	wt := m.detailWorktree

	statusText, statusStyle, activeAgentCount := m.worktreeDetailStatus(wt)
	m.renderWorktreeDetailHeader(&content, wt, statusText, statusStyle)
	m.renderWorktreeDetailSummary(&content, wt)
	m.renderWorktreeDetailGit(&content, wt)
	m.renderWorktreeDetailAgents(&content, wt, activeAgentCount)
	m.renderWorktreeDetailTasks(&content, wt)
	m.renderWorktreeDetailPath(&content, wt)

	return m.applyScroll(content.String(), m.wrapHelpText("j/k:scroll  [a]ttach [e]nvim [s]hell [p]lan [c]ontext [L]ogs [n]ew agent  Esc/q:close"))
}

func (m Model) worktreeDetailStatus(wt *control.WorktreeInfo) (string, lipgloss.Style, int) {
	statusText := "IDLE"
	statusStyle := tui.StyleMuted
	activeAgentCount := 0

	for _, a := range m.agents {
		if a.WorktreePath != wt.Path {
			continue
		}
		activeAgentCount++
		switch a.Status {
		case "running", "executing":
			statusText = "RUNNING"
			statusStyle = tui.StyleSuccess
		case "planning":
			if statusText != "RUNNING" {
				statusText = "PLANNING"
				statusStyle = tui.StyleInfo
			}
		case "awaiting":
			if statusText == "IDLE" {
				statusText = "AWAITING"
				statusStyle = tui.StyleWarning
			}
		}
	}

	if activeAgentCount == 0 {
		switch wt.WTStatus {
		case "merged":
			statusText = "MERGED"
			statusStyle = tui.StyleNeutral
		case "stale":
			statusText = "STALE"
			statusStyle = tui.StyleWarning
		}
	}

	return statusText, statusStyle, activeAgentCount
}

func (m Model) renderWorktreeDetailHeader(content *strings.Builder, wt *control.WorktreeInfo, statusText string, statusStyle lipgloss.Style) {
	branchStyle := lipgloss.NewStyle().Bold(true).Foreground(tui.ColorFg)
	statusPill := formatStatusPill(statusText, statusStyle)

	available := m.width - lipgloss.Width(statusPill) - 4
	if available < 0 {
		available = 0
	}

	headerLeft := branchStyle.Render(truncateText(wt.Branch, available))
	if wt.TicketID != "" {
		ticketPart := tui.StyleMuted.Render(" | ") + tui.StyleAccent.Render(wt.TicketID)
		ticketWidth := lipgloss.Width(ticketPart)
		branchMax := available - ticketWidth
		if branchMax < 4 {
			headerLeft = branchStyle.Render(truncateText(wt.Branch, available))
		} else {
			headerLeft = branchStyle.Render(truncateText(wt.Branch, branchMax)) + ticketPart
		}
	}

	headerPadding := m.width - lipgloss.Width(headerLeft) - lipgloss.Width(statusPill) - 4
	if headerPadding < 2 {
		headerPadding = 2
	}

	content.WriteString("  " + headerLeft + strings.Repeat(" ", headerPadding) + statusPill + "\n")
	content.WriteString(tui.Divider(m.width))
	content.WriteString("\n\n")
}

func (m Model) renderWorktreeDetailSummary(content *strings.Builder, wt *control.WorktreeInfo) {
	desc := wt.Summary
	if desc == "" {
		desc = wt.Description
	}
	if desc == "" {
		return
	}
	content.WriteString(tui.StyleMuted.Render("  Summary:\n"))
	for _, line := range wrapText(desc, m.width-8) {
		content.WriteString("    " + line + "\n")
	}
	content.WriteString("\n")
}

func (m Model) renderWorktreeDetailGit(content *strings.Builder, wt *control.WorktreeInfo) {
	gitStatus := wt.Status
	if strings.Contains(gitStatus, "dirty") {
		gitStatus = tui.StyleWarning.Render("dirty")
	} else if strings.Contains(gitStatus, "clean") {
		gitStatus = tui.StyleSuccess.Render("clean")
	}
	content.WriteString(tui.StyleMuted.Render("  Git: "))
	content.WriteString(wt.Project + " | " + gitStatus)
	content.WriteString("\n\n")
	content.WriteString(tui.Divider(m.width))
	content.WriteString("\n\n")
}

func (m Model) renderWorktreeDetailAgents(content *strings.Builder, wt *control.WorktreeInfo, activeAgentCount int) {
	agentLabel := "AGENTS"
	if activeAgentCount > 0 {
		agentLabel += fmt.Sprintf("  %s", tui.StyleAccent.Render(fmt.Sprintf("%d active", activeAgentCount)))
	}
	content.WriteString(tui.StyleHeader.Render("  " + agentLabel))
	content.WriteString("\n")

	table := m.newAgentTable(agentTableConfig{
		showProject:  false,
		showWorktree: false,
		showActivity: true,
	})
	content.WriteString(table.RenderHeader())
	content.WriteString("\n")

	foundAgents := false
	for _, a := range m.agents {
		if a.WorktreePath != wt.Path {
			continue
		}
		foundAgents = true
		content.WriteString(m.renderAgentTableRow(a, table, false))
		content.WriteString("\n")
	}

	if !foundAgents {
		content.WriteString(tui.StyleMuted.Render("   No agents on this worktree."))
		content.WriteString("\n")
	}
	content.WriteString("\n")
}

func (m Model) renderWorktreeDetailTasks(content *strings.Builder, wt *control.WorktreeInfo) {
	tasks := m.worktreeTasks(wt.Path)
	if len(tasks) == 0 {
		return
	}
	content.WriteString(tui.Divider(m.width))
	content.WriteString("\n\n")

	_, _, completed := countTaskStatuses(tasks)
	taskLabel := fmt.Sprintf("TASKS  %s", tui.StyleMuted.Render(fmt.Sprintf("%d/%d complete", completed, len(tasks))))
	content.WriteString(tui.StyleHeader.Render("  " + taskLabel))
	content.WriteString("\n\n")

	for _, t := range tasks {
		m.renderWorktreeTask(content, t)
	}
	content.WriteString("\n")
}

func countTaskStatuses(tasks []*control.TaskInfo) (int, int, int) {
	pending := 0
	inProgress := 0
	completed := 0
	for _, t := range tasks {
		switch t.Status {
		case "pending":
			pending++
		case "in_progress":
			inProgress++
		case "completed":
			completed++
		}
	}
	return pending, inProgress, completed
}

func (m Model) renderWorktreeDetailPath(content *strings.Builder, wt *control.WorktreeInfo) {
	label := "  Path: "
	maxLen := max(3, m.width-len(label))
	content.WriteString(tui.StyleMuted.Render(label))
	content.WriteString(truncatePathSafe(wt.Path, maxLen))
	content.WriteString("\n")
}

// renderWorktreeAgent renders a single agent line in worktree detail.
func (m Model) renderWorktreeAgent(b *strings.Builder, a *control.AgentInfo) {
	// Status indicator (text-based, no emoji)
	var statusChar string
	switch a.Status {
	case "running", "executing":
		statusChar = tui.StyleSuccess.Render("*")
	case "planning":
		statusChar = tui.StyleInfo.Render("~")
	case "awaiting":
		statusChar = tui.StyleWarning.Render("?")
	case "completed":
		statusChar = tui.StyleMuted.Render("-")
	case "crashed":
		statusChar = tui.StyleDanger.Render("!")
	default:
		statusChar = tui.StyleMuted.Render("-")
	}

	// Metrics
	tokens := "-"
	cache := ""
	if a.Metrics != nil {
		if a.Metrics.TotalTokens > 0 {
			tokens = formatCompactNumber(a.Metrics.TotalTokens)
		}
		if a.Metrics.CacheReads > 0 {
			cache = fmt.Sprintf(" (%s cached)", tui.StyleSuccess.Render(formatCompactNumber(a.Metrics.CacheReads)))
		}
	}

	age := formatDuration(time.Since(parseCreatedAt(a.CreatedAt)))
	activity := a.LastActivity
	if activity == "" {
		activity = a.Status
	}

	// Line 1: status + archetype + id + tokens
	b.WriteString(fmt.Sprintf("    %s %s %s", statusChar, a.Archetype, shortID(a.ID, 8)))
	b.WriteString(tui.StyleMuted.Render(fmt.Sprintf("  %s tokens%s", tokens, cache)))
	b.WriteString("\n")

	// Line 2: activity + age
	ageSuffix := fmt.Sprintf("  %s ago", age)
	activityMax := m.width - lipgloss.Width("      ") - lipgloss.Width(ageSuffix)
	if activityMax < 0 {
		activityMax = 0
	}
	activity = truncateText(activity, activityMax)
	b.WriteString(tui.StyleMuted.Render("      " + activity))
	b.WriteString(tui.StyleMuted.Render(ageSuffix))
	b.WriteString("\n")
}

// renderWorktreeTask renders a single task line in worktree detail.
func (m Model) renderWorktreeTask(b *strings.Builder, t *control.TaskInfo) {
	// Status indicator (text-based, no emoji)
	var statusChar string
	switch t.Status {
	case "completed":
		statusChar = tui.StyleSuccess.Render("+")
	case "in_progress":
		statusChar = tui.StyleAccent.Render("*")
	default:
		statusChar = tui.StyleMuted.Render("o")
	}

	// Subject
	subject := t.Subject
	if t.ActiveForm != "" && t.Status == "in_progress" {
		subject = t.ActiveForm
	}

	subjectStyle := lipgloss.NewStyle().Foreground(tui.ColorFg)
	if t.Status == "completed" {
		subjectStyle = lipgloss.NewStyle().Foreground(tui.ColorFgMuted)
	}

	prefix := fmt.Sprintf("    %s ", statusChar)
	suffix := ""

	// Status label on right
	statusLabel := ""
	switch t.Status {
	case "completed":
		statusLabel = tui.StyleMuted.Render("done")
	case "in_progress":
		statusLabel = tui.StyleAccent.Render("in progress")
	default:
		if len(t.BlockedBy) > 0 {
			statusLabel = tui.StyleWarning.Render("blocked")
		}
	}
	if statusLabel != "" {
		suffix = "  " + statusLabel
	}

	subjectMax := m.width - lipgloss.Width(prefix) - lipgloss.Width(suffix)
	if subjectMax < 0 {
		subjectMax = 0
	}
	subject = truncateText(subject, subjectMax)

	b.WriteString(prefix + subjectStyle.Render(subject))
	if suffix != "" {
		b.WriteString(suffix)
	}
	b.WriteString("\n")
}

// worktreeTasks returns tasks from task lists matching the worktree path.
func (m Model) worktreeTasks(wtPath string) []*control.TaskInfo {
	var result []*control.TaskInfo

	// Find task list matching this worktree
	for _, list := range m.taskLists {
		if list.Path == wtPath {
			// Return tasks from this list
			for _, t := range m.claudeTasks {
				if t.ListID == list.ID {
					result = append(result, t)
				}
			}
			break
		}
	}

	return result
}

func (m Model) renderLogs() (string, string) {

	var content strings.Builder

	// Header with follow indicator

	icon := tui.Logo()

	content.WriteString(icon + " " + tui.StyleLogo.Render("Agent Logs"))

	content.WriteString(tui.StyleMuted.Render(" │ " + shortID(m.logsAgentID, 8)))

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

	return content.String(), m.wrapHelpText(helpText)

}

func (m Model) renderPlanView() (string, string) {
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

		viewportLines := lines[startIdx:endIdx]
		content.WriteString(strings.Join(viewportLines, "\n"))

		// Scroll indicator
		if totalLines > viewportHeight {
			content.WriteString("\n")
			position := fmt.Sprintf(" [%d-%d of %d lines]", startIdx+1, endIdx, totalLines)
			content.WriteString(tui.StyleMuted.Render(position))
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
	return content.String(), m.wrapHelpText(helpText)
}

func (m Model) renderWorktreeWizard() (string, string) {
	var content strings.Builder

	m.renderWizardHeader(&content, "New Worktree")
	content.WriteString(renderStepIndicator([]string{"Ticket", "Description", "Project", "Workflow"}, m.worktreeStep))
	content.WriteString("\n\n")
	m.renderWorktreeWizardStep(&content)

	return content.String(), m.wrapHelpText("Enter to continue · Esc to cancel")
}

func (m Model) renderPromoteWizard() (string, string) {
	var content strings.Builder

	m.renderWizardHeader(&content, "Promote to Feature")
	content.WriteString(renderStepIndicator([]string{"Project", "Agent"}, m.promoteStep))
	content.WriteString("\n\n")

	// Show note content (always visible but muted)
	content.WriteString(tui.StyleMuted.Render("  Note: "))
	// Truncate note for display
	notePreview := m.promoteNoteText
	if len(notePreview) > 60 {
		notePreview = notePreview[:57] + "..."
	}
	content.WriteString(tui.StyleMuted.Render(notePreview))
	content.WriteString("\n\n")

	if m.promoteStep == 0 {
		// Step 1: Project selection
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
	} else {
		// Step 2: Agent selection
		content.WriteString(tui.StyleMuted.Render("  Select Agent Harness (j/k to move, Enter to confirm):"))
		content.WriteString("\n\n")

		for i, provider := range promoteProviderLabels {
			if i == m.promoteAgentIdx {
				content.WriteString(tui.StyleSelectedIndicator.Render("  → "))
				content.WriteString(tui.StyleSelected.Render(provider))
			} else {
				content.WriteString("    ")
				content.WriteString(provider)
			}
			content.WriteString("\n")
		}
	}

	return content.String(), m.wrapHelpText("Enter to continue · Backspace to go back · Esc to cancel")
}

func (m Model) renderWizardHeader(content *strings.Builder, title string) {
	icon := tui.Logo()
	content.WriteString(icon + " " + tui.StyleLogo.Render(title))
	content.WriteString("\n")
	content.WriteString(tui.Divider(m.width))
	content.WriteString("\n\n")
}

func renderStepIndicator(steps []string, current int) string {
	var stepIndicator []string
	for i, step := range steps {
		if i < current {
			stepIndicator = append(stepIndicator, tui.StyleSuccessMsg.Render("✓ "+step))
		} else if i == current {
			stepIndicator = append(stepIndicator, tui.StyleAccent.Render("→ "+step))
		} else {
			stepIndicator = append(stepIndicator, tui.StyleMuted.Render("○ "+step))
		}
	}
	return "  " + strings.Join(stepIndicator, "  ")
}

func (m Model) renderWorktreeWizardStep(content *strings.Builder) {
	switch m.worktreeStep {
	case 0:
		m.renderWorktreeWizardTicket(content)
	case 1:
		m.renderWorktreeWizardDescription(content)
	case 2:
		m.renderWorktreeWizardProject(content)
	case 3:
		m.renderWorktreeWizardWorkflow(content)
	}
}

func (m Model) renderWorktreeWizardTicket(content *strings.Builder) {
	content.WriteString(tui.StyleMuted.Render("  Enter a ticket ID (e.g., ENG-123) or press Tab/Enter to skip:"))
	content.WriteString("\n\n")
	content.WriteString("  ")
	content.WriteString(m.textInput.View())
	content.WriteString("\n")
}

func (m Model) renderWorktreeWizardDescription(content *strings.Builder) {
	if m.worktreeTicketID != "" {
		content.WriteString(tui.StyleMuted.Render(ticketLabel))
		content.WriteString(tui.StyleAccent.Render(m.worktreeTicketID))
		content.WriteString("\n\n")
	}
	content.WriteString(tui.StyleMuted.Render("  Enter a brief description for this worktree:"))
	content.WriteString("\n\n")
	content.WriteString("  ")
	content.WriteString(m.textInput.View())
	content.WriteString("\n")
}

func (m Model) renderWorktreeWizardProject(content *strings.Builder) {
	if m.worktreeTicketID != "" {
		content.WriteString(tui.StyleMuted.Render(ticketLabel))
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

func (m Model) renderWorktreeWizardWorkflow(content *strings.Builder) {
	if m.worktreeTicketID != "" {
		content.WriteString(tui.StyleMuted.Render(ticketLabel))
		content.WriteString(tui.StyleAccent.Render(m.worktreeTicketID))
		content.WriteString("\n")
	}
	content.WriteString(tui.StyleMuted.Render("  Project: "))
	content.WriteString(tui.StyleAccent.Render(m.projects[m.worktreeProjectIdx]))
	content.WriteString("\n\n")

	content.WriteString(tui.StyleMuted.Render("  Select workflow mode (j/k to cycle, Enter to confirm):"))
	content.WriteString("\n\n")

	desc, style := workflowModeDescription(m.worktreeWorkflow)
	content.WriteString(tui.StyleSelectedIndicator.Render("  → "))
	content.WriteString(style.Bold(true).Render(string(m.worktreeWorkflow)))
	content.WriteString("\n")
	content.WriteString(tui.StyleMuted.Render("    " + desc))
	content.WriteString("\n")
}

func workflowModeDescription(mode config.WorkflowMode) (string, lipgloss.Style) {
	switch mode {
	case config.WorkflowModeAutomatic:
		return "Agent works autonomously until complete.", tui.StyleSuccess
	case config.WorkflowModeApprove:
		return "Agent asks for approval before executing changes.", tui.StyleWarning
	case config.WorkflowModeManual:
		return "User drives the agent manually.", tui.StyleMuted
	default:
		return "", tui.StyleMuted
	}
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

func truncateText(text string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}

func truncatePathSafe(path string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if maxLen < 4 {
		return truncateText(path, maxLen)
	}
	return truncatePath(path, maxLen)
}

func (m Model) wrapHelpText(help string) string {
	help = strings.TrimSpace(help)
	if help == "" {
		return ""
	}

	maxWidth := max(1, m.width-2)
	lines := wrapText(help, maxWidth)
	if len(lines) == 0 {
		return ""
	}

	for i, line := range lines {
		lines[i] = tui.StyleHelp.Render("  " + line)
	}

	return strings.Join(lines, "\n")
}

// applyInputOverlay renders the input panel as a bottom overlay
func (m Model) applyInputOverlay(content string) string {
	// The content passed here was rendered with m.height (which should be reduced when input is active)
	// We just need to append the input panel at the bottom.
	// Since m.height + inputPanelHeight = m.termHeight, we can just join them.

	// However, PinFooterToBottom was used before to ensure alignment.
	// If we trust m.height is correct for content, we can just append.
	// But let's use PinFooterToBottom with full termHeight to be safe and handle any off-by-one errors.

	inputPanel := m.renderInputPanel()
	return layout.PinFooterToBottom(content, inputPanel, m.termHeight)
}

// renderInputPanel renders the input panel content wrapped in a sleek box
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
	b.WriteString(tui.StyleMuted.Render("Enter to send · Shift+Enter for newline · Esc to cancel"))

	// Wrap in sleek box
	// Use m.termWidth for the full width box
	return tui.StyleInputBox.
		Width(m.termWidth - 2). // -2 for borders (left/right padding handled by style)
		Render(b.String())
}

func (m *Model) syncInputHeight() {
	// Allow input to grow up to half the screen
	maxHeight := m.termHeight / 2
	if maxHeight < inputHeightMax {
		maxHeight = inputHeightMax
	}

	inputContentHeight := min(maxHeight, max(inputHeightMin, m.textInput.LineCount()))
	if inputContentHeight != m.textInput.Height() {
		m.textInput.SetHeight(inputContentHeight)
	}

	// Calculate total input panel height
	// Content = input lines + footer line (1)
	// Box = +2 (border top/bottom)
	totalInputHeight := inputContentHeight + 1 + 2

	// Update available content height
	if m.inputMode {
		m.height = m.termHeight - totalInputHeight
	} else {
		m.height = m.termHeight
	}

	// Ensure positive height
	if m.height < 1 {
		m.height = 1
	}
}

func (m *Model) resetTextInput() {
	m.textInput.Reset()
	m.syncInputHeight()
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

func (m *Model) doLogs() tea.Cmd {
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

func (m Model) doAbandon(wt *control.WorktreeInfo) tea.Cmd {
	return func() tea.Msg {
		err := m.client.AbandonWorktree(wt.Path)
		if err != nil {
			return errMsg(err)
		}
		return abandonResultMsg{path: wt.Path}
	}
}

func (m *Model) doPlanView() tea.Cmd {
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

func (m *Model) fetchPlan(worktreePath string, forceRefresh bool) tea.Cmd {
	m.planWorktreePath = worktreePath
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

func (m *Model) doContextView() tea.Cmd {
	var wts []*control.WorktreeInfo
	var agents []*control.AgentInfo

	if m.level == LevelDashboard {
		wts = m.worktrees
		agents = m.agents
	} else {
		wts = m.projectWorktrees()
		agents = m.projectAgents()
	}

	var worktreePath, projectName string
	switch m.tab {
	case TabWorktrees:
		if m.selected < len(wts) {
			worktreePath = wts[m.selected].Path
			projectName = wts[m.selected].Project
		}
	case TabAgents:
		if m.selected < len(agents) {
			worktreePath = agents[m.selected].WorktreePath
			projectName = agents[m.selected].ProjectName
		}
	}

	if worktreePath != "" {
		return m.fetchContext(worktreePath, projectName)
	}
	return nil
}

func (m *Model) fetchContext(worktreePath, projectName string) tea.Cmd {
	m.contextWorktreePath = worktreePath
	m.contextProjectName = projectName
	return func() tea.Msg {
		// Fetch blackboard entries
		blackboard, err := m.client.GetBlackboard(worktreePath)
		if err != nil {
			return errMsg(fmt.Errorf("failed to get blackboard: %w", err))
		}

		// Fetch blackboard summary
		summary, err := m.client.GetBlackboardSummary(worktreePath)
		if err != nil {
			// Non-fatal, continue without summary
			summary = nil
		}

		// Fetch context preview
		preview, err := m.client.GetContextPreview(worktreePath, projectName)
		if err != nil {
			// Non-fatal, generate from blackboard
			preview = ""
		}

		return contextResultMsg{
			worktreePath: worktreePath,
			projectName:  projectName,
			blackboard:   blackboard,
			summary:      summary,
			preview:      preview,
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
		return m.showStatus("Select an agent to retry")
	}

	agent := agents[m.selected]

	// Allow retry for crashed/terminated agents OR completed planners
	canRetry := agent.Status == "crashed" || agent.Status == "terminated"
	if agent.Archetype == "planner" && agent.Status == "completed" {
		canRetry = true
	}

	if !canRetry {
		return m.showStatus("Only crashed/terminated agents or completed planners can be retried")
	}

	return m.respawnAgent(agent)
}

func (m Model) respawnAgent(agent *control.AgentInfo) tea.Cmd {
	return func() tea.Msg {
		// Use cascade delete to clean up agent and all dependent records (including plans)
		_ = m.client.KillAgentWithDelete(agent.ID, true)

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

// doQuickApprove approves a draft plan directly from the agents tab.
func (m Model) doQuickApprove() tea.Cmd {
	var agents []*control.AgentInfo

	if m.level == LevelDashboard {
		agents = m.agents
	} else {
		agents = m.projectAgents()
	}

	if m.selected >= len(agents) {
		return m.showStatus("No agent selected")
	}

	agent := agents[m.selected]

	// Must be a planner with a draft plan
	if agent.Archetype != "planner" {
		return m.showStatus("Approve only works on planner agents")
	}
	if agent.PlanStatus != "draft" {
		return m.showStatus("Plan must be in draft status to approve")
	}

	return m.approvePlan(agent.WorktreePath)
}

// doQuickExecute spawns an executor for an approved plan directly from agents tab.
func (m Model) doQuickExecute() tea.Cmd {
	var agents []*control.AgentInfo

	if m.level == LevelDashboard {
		agents = m.agents
	} else {
		agents = m.projectAgents()
	}

	if m.selected >= len(agents) {
		return m.showStatus("No agent selected")
	}

	agent := agents[m.selected]

	// Must be a planner with an approved plan
	if agent.Archetype != "planner" {
		return m.showStatus("Execute only works on planner agents")
	}
	if agent.PlanStatus != "approved" {
		if agent.PlanStatus == "draft" {
			return m.showStatus("Plan must be approved first - press [A] to approve")
		}
		return m.showStatus("No approved plan to execute")
	}

	return m.spawnExecutor(agent.WorktreePath)
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

	res := fetchDataResultMsg{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var errs []error

	m.fetchWorktrees(&res, &wg, &mu, &errs)
	m.fetchAgents(&res, &wg, &mu, &errs)
	m.fetchJobs(&res, &wg, &mu, &errs)
	m.fetchNotes(&res, &wg, &mu, &errs)
	m.fetchChangelog(&res, &wg, &mu, &errs)
	m.fetchTaskListsAndTasks(&res, &wg, &mu)

	wg.Wait()

	if len(errs) > 0 {
		res.err = errors.Join(errs...)
	}

	return res
}

func (m Model) fetchWorktrees(res *fetchDataResultMsg, wg *sync.WaitGroup, mu *sync.Mutex, errs *[]error) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		wts, err := m.client.ListWorktrees()
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			*errs = append(*errs, fmt.Errorf("list worktrees: %w", err))
			res.worktrees = m.worktrees
			return
		}
		res.worktrees = wts
	}()
}

func (m Model) fetchAgents(res *fetchDataResultMsg, wg *sync.WaitGroup, mu *sync.Mutex, errs *[]error) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		agents, err := m.client.ListAgents()
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			*errs = append(*errs, fmt.Errorf("list agents: %w", err))
			res.agents = m.agents
			return
		}
		res.agents = agents
	}()
}

func (m Model) fetchJobs(res *fetchDataResultMsg, wg *sync.WaitGroup, mu *sync.Mutex, errs *[]error) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		jobs, err := m.client.ListJobs()
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			*errs = append(*errs, fmt.Errorf("list jobs: %w", err))
			res.jobs = m.jobs
			return
		}
		res.jobs = jobs
	}()
}

func (m Model) fetchNotes(res *fetchDataResultMsg, wg *sync.WaitGroup, mu *sync.Mutex, errs *[]error) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		notes, err := m.client.ListNotes()
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			*errs = append(*errs, fmt.Errorf("list notes: %w", err))
			res.notes = m.notes
			return
		}
		res.notes = notes
	}()
}

func (m Model) fetchChangelog(res *fetchDataResultMsg, wg *sync.WaitGroup, mu *sync.Mutex, errs *[]error) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		changelog, err := m.client.ListChangelog("", 100)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			*errs = append(*errs, fmt.Errorf("list changelog: %w", err))
			res.changelog = m.changelog
			return
		}
		res.changelog = changelog
	}()
}

func (m Model) fetchTaskListsAndTasks(res *fetchDataResultMsg, wg *sync.WaitGroup, mu *sync.Mutex) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		taskLists, err := m.client.ListTaskLists()
		mu.Lock()
		if err != nil {
			res.taskLists = m.taskLists
		} else {
			res.taskLists = taskLists
		}
		mu.Unlock()

		listID := m.resolveTaskListID(res, mu)
		if listID == "" {
			return
		}

		tasks, err := m.client.ListTasks(listID, "")
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			res.claudeTasks = m.claudeTasks
			return
		}
		res.claudeTasks = tasks
	}()
}

func (m Model) resolveTaskListID(res *fetchDataResultMsg, mu *sync.Mutex) string {
	mu.Lock()
	defer mu.Unlock()
	listID := m.selectedTaskList
	if listID == "" && len(res.taskLists) > 0 {
		listID = res.taskLists[0].ID
	}
	return listID
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

func (m *Model) fetchLogs(agentID string) tea.Cmd {
	m.logsAgentID = agentID
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

func shortID(id string, maxLen int) string {
	if len(id) <= maxLen {
		return id
	}
	return id[:maxLen] + "..."
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

func (m Model) fetchDetailPlan(agentID, worktreePath string) tea.Cmd {
	return func() tea.Msg {
		plan, err := m.client.GetPlan(worktreePath, false)
		if err != nil {
			return detailPlanResultMsg{
				agentID: agentID,
				content: "",
			}
		}
		return detailPlanResultMsg{
			agentID: agentID,
			content: plan.Content,
		}
	}
}

func (m Model) viewAgent(agent *control.AgentInfo) (Model, tea.Cmd) {
	m.detailAgent = agent
	m.detailMode = true
	var cmd tea.Cmd

	if agent.Prompt != "" {
		m.detailRenderedPrompt = "Rendering..." // Placeholder
		cmd = tea.Batch(cmd, m.renderMarkdownCmd("agent:"+agent.ID, agent.Prompt))
	} else {
		m.detailRenderedPrompt = ""
	}

	m.detailRenderedPlan = ""
	if agent.Archetype == "planner" {
		m.detailRenderedPlan = "Loading plan..."
		cmd = tea.Batch(cmd, m.fetchDetailPlan(agent.ID, agent.WorktreePath))
	}

	return m, cmd
}

