package dashboard

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) handleWindowSize(msg tea.WindowSizeMsg) (Model, tea.Cmd) {
	m.termWidth = msg.Width
	m.termHeight = msg.Height

	// Inner dimensions match terminal initially (reduced when input is active)
	m.width = msg.Width
	m.height = msg.Height

	// Update text input width dynamically
	inputWidth := m.width - 10
	if inputWidth < 30 {
		inputWidth = 30
	}
	if inputWidth > 100 {
		inputWidth = 100
	}
	m.textInput.SetWidth(inputWidth)
	m.syncInputHeight()

	// Update table widths
	m.worktreeTable.SetWidth(m.width)
	m.jobTable.SetWidth(m.width)
	m.agentTable.SetWidth(m.width)
	m.taskTable.SetWidth(m.width)

	var cmd tea.Cmd

	if m.planMode {
		if m.planStatus == "pending" {
			m.planRendered = m.renderPendingPlan(m.planPlannerStatus)
		} else {
			m.planRendered = "Rendering..."
			cmd = tea.Batch(cmd, m.renderMarkdownCmd("plan", m.planContent))
		}
	}

	if m.detailMode && m.detailAgent != nil && m.detailAgent.Prompt != "" {
		m.detailRenderedPrompt = "Rendering..."
		cmd = tea.Batch(cmd, m.renderMarkdownCmd("agent:"+m.detailAgent.ID, m.detailAgent.Prompt))
	}
	return m, cmd
}

func (m Model) handleFetchDataResult(msg fetchDataResultMsg) (Model, tea.Cmd) {
	m.worktrees = msg.worktrees
	m.agents = msg.agents
	m.jobs = msg.jobs
	m.notes = msg.notes
	m.changelog = msg.changelog
	m.taskLists = msg.taskLists
	m.claudeTasks = msg.claudeTasks
	m.projects = m.extractProjects()
	m.lastUpdate = time.Now()

	// Update detail view if active
	var cmd tea.Cmd
	if m.detailMode && m.detailAgent != nil {
		found := false
		for _, a := range msg.agents {
			if a.ID == m.detailAgent.ID {
				if a.Prompt != m.detailAgent.Prompt {
					m.detailRenderedPrompt = "Rendering..."
					cmd = tea.Batch(cmd, m.renderMarkdownCmd("agent:"+a.ID, a.Prompt))
				}
				m.detailAgent = a
				found = true
				break
			}
		}
		// If agent no longer exists (e.g. deleted), exit detail mode
		if !found {
			m.detailMode = false
			m.detailAgent = nil
		}
	}

	if msg.err != nil {
		m.err = msg.err
		m.statusMsg = "✗ " + msg.err.Error()
		m.statusMsgTime = time.Now()
		cmd = tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
			return clearErrMsg{}
		})
	}
	return m, cmd
}

func (m Model) handleErr(msg errMsg) (Model, tea.Cmd) {
	m.err = msg
	// Also set as status message for more visibility
	m.statusMsg = "✗ " + msg.Error()
	m.statusMsgTime = time.Now()
	// Auto-clear error after 5 seconds
	return m, tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return clearErrMsg{}
	})
}

func (m Model) handleLogsResult(msg logsResultMsg) (Model, tea.Cmd) {
	if m.logsAgentID == "" || msg.agentID != m.logsAgentID {
		return m, nil
	}
	m.logsAgentID = msg.agentID
	m.logs = msg.logs
	m.logsMode = true
	m.logsFollow = true
	// Start at bottom (most recent) if following
	m.logsScroll = max(0, len(m.logs)-m.logsViewportHeight())
	return m, nil
}

func (m Model) handlePlanResult(msg planResultMsg) (Model, tea.Cmd) {
	if m.planWorktreePath == "" || msg.worktreePath != m.planWorktreePath {
		return m, nil
	}
	m.inputMode = false
	m.questionMode = false
	m.noteMode = false
	m.worktreeMode = false
	m.promoteMode = false
	m.detailMode = false
	m.logsMode = false
	m.contextMode = false
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
		return m, nil
	}
	m.planRendered = "Rendering..."
	return m, m.renderMarkdownCmd("plan", msg.content)
}

func (m Model) handleContextResult(msg contextResultMsg) (Model, tea.Cmd) {
	if m.contextWorktreePath == "" || msg.worktreePath != m.contextWorktreePath {
		return m, nil
	}
	m.contextWorktreePath = msg.worktreePath
	m.contextProjectName = msg.projectName
	m.contextBlackboard = msg.blackboard
	m.contextSummary = msg.summary
	m.contextPreview = msg.preview
	m.contextMode = true
	m.contextScroll = 0
	return m, nil
}

func (m Model) handlePublishResult(msg publishResultMsg) (Model, tea.Cmd) {
	m.statusMsg = fmt.Sprintf("PR created: %s", msg.prURL)
	m.statusMsgTime = time.Now()
	return m, tea.Batch(m.fetchData, tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		return clearStatusMsg{}
	}))
}

func (m Model) handleMergeResult(msg mergeResultMsg) (Model, tea.Cmd) {
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
	return m, tea.Batch(m.fetchData, tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		return clearStatusMsg{}
	}))
}

func (m Model) handleCleanupResult(msg cleanupResultMsg) (Model, tea.Cmd) {
	m.statusMsg = fmt.Sprintf("Worktree cleaned up: %s", filepath.Base(msg.path))
	m.statusMsgTime = time.Now()
	return m, tea.Batch(m.fetchData, tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return clearStatusMsg{}
	}))
}

func (m Model) handleSpinnerTick(msg spinner.TickMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m Model) handleControlEvent(msg eventMsg) (Model, tea.Cmd) {
	return m, tea.Batch(m.fetchData, m.listenForEvents())
}

func (m Model) handleMarkdownRendered(msg markdownRenderedMsg) (Model, tea.Cmd) {
	if strings.HasPrefix(msg.target, "agent:") {
		agentID := strings.TrimPrefix(msg.target, "agent:")
		if m.detailMode && m.detailAgent != nil && m.detailAgent.ID == agentID {
			m.detailRenderedPrompt = msg.content
		}
	} else if strings.HasPrefix(msg.target, "agent_plan:") {
		agentID := strings.TrimPrefix(msg.target, "agent_plan:")
		if m.detailMode && m.detailAgent != nil && m.detailAgent.ID == agentID {
			m.detailRenderedPlan = msg.content
		}
	} else if msg.target == "plan" {
		if m.planMode {
			m.planRendered = msg.content
		}
	}
	return m, nil
}

func (m Model) handleDetailPlanResult(msg detailPlanResultMsg) (Model, tea.Cmd) {
	if m.detailMode && m.detailAgent != nil && m.detailAgent.ID == msg.agentID {
		m.detailRenderedPlan = "Rendering..."
		return m, m.renderMarkdownCmd("agent_plan:"+msg.agentID, msg.content)
	}
	return m, nil
}
