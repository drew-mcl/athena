package dashboard

// TabView renders a tab's content for the current model state.
type TabView interface {
	Render(m *Model) string
}

// TabViewFunc adapts a function to the TabView interface.
type TabViewFunc func(*Model) string

// Render implements TabView.
func (f TabViewFunc) Render(m *Model) string {
	return f(m)
}

var dashboardTabViews = map[Tab]TabView{
	TabProjects:  TabViewFunc(func(m *Model) string { return m.renderProjects() }),
	TabWorktrees: TabViewFunc(func(m *Model) string { return m.renderAllWorktrees() }),
	TabJobs:      TabViewFunc(func(m *Model) string { return m.renderAllAgents() }),
	TabTasks:     TabViewFunc(func(m *Model) string { return m.renderClaudeTasks() }),
	TabQuestions: TabViewFunc(func(m *Model) string { return m.renderQuestions() }),
	TabAdmin:     TabViewFunc(func(m *Model) string { return m.renderAdmin() }),
}

var projectTabViews = map[Tab]TabView{
	TabWorktrees: TabViewFunc(func(m *Model) string { return m.renderWorktrees() }),
	TabAgents:    TabViewFunc(func(m *Model) string { return m.renderAgents() }),
	TabTasks:     TabViewFunc(func(m *Model) string { return m.renderTasks() }),
	TabNotes:     TabViewFunc(func(m *Model) string { return m.renderNotes() }),
}

func (m *Model) renderDashboardContent() string {
	view := dashboardTabViews[m.tab]
	if view == nil {
		return ""
	}
	return view.Render(m)
}

func (m *Model) renderDetailContent() string {
	view := projectTabViews[m.tab]
	if view == nil {
		return ""
	}
	return view.Render(m)
}
