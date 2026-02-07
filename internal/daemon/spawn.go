package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/drewfead/athena/internal/agent"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/logging"
	"github.com/drewfead/athena/internal/plugin"
	"github.com/drewfead/athena/internal/plugin/pm"
	"github.com/drewfead/athena/internal/store"
	"github.com/drewfead/athena/internal/worktree"
)

// handleSpawn is the unified spawn handler.
// Primary flow: feature ID -> lookup feature + parent goal -> create worktree from queue head -> launch agent.
// Also supports: ticket IDs, work item IDs, or bare spawns.
func (d *Daemon) handleSpawn(params json.RawMessage) (any, error) {
	var req control.SpawnRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	project := req.Project
	if project == "" {
		project = "default"
	}

	workItem, parentGoal, ticketContext, err := d.resolveSpawnTarget(req, project)
	if err != nil {
		return nil, err
	}
	project = workItem.Project

	taskListID := workItem.ID
	prompt := d.buildSpawnPrompt(workItem, parentGoal, ticketContext, taskListID, req.Retrieve)

	archetype := "executor"
	if req.Retrieve {
		archetype = "planner"
	}

	resp := &control.SpawnResponse{
		WorkItem:   workItemToInfo(workItem),
		TaskListID: taskListID,
	}

	workDir := req.WorkDir
	if workItem.WorktreePath != nil && *workItem.WorktreePath != "" {
		workDir = *workItem.WorktreePath
	}

	if req.Headless {
		if err := d.spawnHeadless(workItem, workDir, project, archetype, prompt, taskListID, resp); err != nil {
			return nil, err
		}
	} else {
		resp.ExecArgs, resp.ExecEnv = d.buildInteractiveExec(prompt, archetype, taskListID)
	}

	if workItem.WorktreePath != nil {
		wt, err := d.store.GetWorktree(*workItem.WorktreePath)
		if err == nil && wt != nil {
			resp.Worktree = &control.WorktreeInfo{
				Path:    wt.Path,
				Project: wt.Project,
				Branch:  wt.Branch,
				IsMain:  wt.IsMain,
			}
		}
	}

	return resp, nil
}

// resolveSpawnTarget determines the work item, parent goal, and ticket context
// based on the spawn request mode (feature, ticket, work item, or bare).
func (d *Daemon) resolveSpawnTarget(req control.SpawnRequest, project string) (*store.WorkItem, *store.WorkItem, string, error) {
	switch {
	case req.FeatureID != "":
		return d.resolveFeatureSpawn(req.FeatureID)
	case req.TicketID != "":
		wi, ctx, err := d.resolveTicket(req.TicketID, project)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to resolve ticket: %w", err)
		}
		return wi, nil, ctx, nil
	case req.WorkItemID != "":
		return d.resolveWorkItemSpawn(req.WorkItemID)
	default:
		wi, err := d.createBareWorkItem(project)
		if err != nil {
			return nil, nil, "", err
		}
		return wi, nil, "", nil
	}
}

// resolveFeatureSpawn looks up a feature, gets its parent goal, creates a worktree
// if needed, and marks the feature as in_progress.
func (d *Daemon) resolveFeatureSpawn(featureID string) (*store.WorkItem, *store.WorkItem, string, error) {
	wi, err := d.store.GetWorkItem(featureID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to get feature: %w", err)
	}
	if wi == nil {
		return nil, nil, "", fmt.Errorf("feature not found: %s", featureID)
	}
	if wi.ItemType != store.WorkItemTypeFeature {
		return nil, nil, "", fmt.Errorf("work item %s is a %s, not a feature", featureID, wi.ItemType)
	}

	var parentGoal *store.WorkItem
	if wi.ParentID != nil {
		goal, err := d.store.GetWorkItem(*wi.ParentID)
		if err == nil && goal != nil {
			parentGoal = goal
		}
	}

	if wi.WorktreePath == nil || *wi.WorktreePath == "" {
		wtPath, err := d.createFeatureWorktree(wi, wi.Project)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to create worktree: %w", err)
		}
		wi.WorktreePath = &wtPath
	}

	wi.Status = store.WorkItemStatusInProgress
	d.store.UpdateWorkItem(wi)

	return wi, parentGoal, "", nil
}

// resolveWorkItemSpawn fetches an existing work item by ID.
func (d *Daemon) resolveWorkItemSpawn(workItemID string) (*store.WorkItem, *store.WorkItem, string, error) {
	wi, err := d.store.GetWorkItem(workItemID)
	if err != nil {
		return nil, nil, "", err
	}
	if wi == nil {
		return nil, nil, "", fmt.Errorf("work item not found: %s", workItemID)
	}
	return wi, nil, "", nil
}

// createBareWorkItem creates an anonymous task work item for bare spawns.
func (d *Daemon) createBareWorkItem(project string) (*store.WorkItem, error) {
	id, err := d.store.GenerateWorkItemID("")
	if err != nil {
		return nil, fmt.Errorf("failed to generate work item ID: %w", err)
	}
	wi := &store.WorkItem{
		ID:       id,
		Project:  project,
		ItemType: store.WorkItemTypeTask,
		Subject:  "Interactive session",
		Status:   store.WorkItemStatusInProgress,
	}
	if err := d.store.CreateWorkItem(wi); err != nil {
		return nil, fmt.Errorf("failed to create work item: %w", err)
	}
	return wi, nil
}

// spawnHeadless launches an agent in headless mode, updates the work item,
// and populates the response with agent info.
func (d *Daemon) spawnHeadless(workItem *store.WorkItem, workDir, project, archetype, prompt, taskListID string, resp *control.SpawnResponse) error {
	if workDir == "" {
		return fmt.Errorf("no work directory specified for headless spawn")
	}

	spawnedAgent, err := d.spawner.Spawn(d.ctx, agent.SpawnSpec{
		WorktreePath: workDir,
		ProjectName:  project,
		Archetype:    archetype,
		Prompt:       prompt,
		TaskListID:   taskListID,
	})
	if err != nil {
		return fmt.Errorf("failed to spawn agent: %w", err)
	}

	agentID := spawnedAgent.ID
	workItem.AgentID = &agentID
	workItem.Status = store.WorkItemStatusInProgress
	d.store.UpdateWorkItem(workItem)

	d.server.Broadcast(control.Event{
		Type:    "agent_created",
		Payload: d.agentToInfo(spawnedAgent),
	})

	resp.Agent = d.agentToInfo(spawnedAgent)
	return nil
}

// createFeatureWorktree creates a worktree for a feature work item.
// Path: ~/repos/worktrees/<project>/<feature-id>, branching from merge queue head.
func (d *Daemon) createFeatureWorktree(feature *store.WorkItem, project string) (string, error) {
	// Find the main repo for this project
	mainRepoPath, err := d.findMainRepoPath(project)
	if err != nil {
		return "", fmt.Errorf("cannot find main repo for project %q: %w", project, err)
	}

	// Get queue head for start point
	startPoint := ""
	queueBranch, _, err := d.getIntegrationHead(project)
	if err == nil && queueBranch != "" {
		startPoint = queueBranch
		logging.Info("using queue head as start point", "branch", queueBranch, "project", project)
	}

	// Create branch name from feature ID
	branch := fmt.Sprintf("feat/%s", feature.ID)

	opts := worktree.CreateWorktreeOptions{
		MainRepoPath: mainRepoPath,
		Branch:       branch,
		TicketID:     feature.ID,
		Description:  feature.Subject,
		WorkflowMode: "manual", // Don't auto-spawn planner - we're spawning our own agent
		StartPoint:   startPoint,
	}

	wtPath, err := d.migrator.CreateWorktree(opts)
	if err != nil {
		return "", err
	}

	// Update the feature work item with the worktree path
	feature.WorktreePath = &wtPath
	d.store.UpdateWorkItem(feature)

	logging.Info("created worktree for feature",
		"feature", feature.ID,
		"worktree", wtPath,
		"branch", branch,
		"start_point", startPoint,
	)

	return wtPath, nil
}

// findMainRepoPath finds the main repo path for a project from the store.
func (d *Daemon) findMainRepoPath(project string) (string, error) {
	worktrees, err := d.store.ListWorktrees(project)
	if err != nil {
		return "", err
	}

	for _, wt := range worktrees {
		if wt.IsMain {
			return wt.Path, nil
		}
	}

	// Fallback: try listing all worktrees and match by project_name
	allWorktrees, err := d.store.ListWorktrees("")
	if err != nil {
		return "", err
	}
	for _, wt := range allWorktrees {
		if wt.IsMain && wt.ProjectName != nil && *wt.ProjectName == project {
			return wt.Path, nil
		}
	}

	return "", fmt.Errorf("no main repo found for project %q", project)
}

// resolveTicket looks up a ticket via PM plugins and creates work items based on issue type.
// Epics become Goals with child Features; Stories/Tasks become Features under their parent Goal.
func (d *Daemon) resolveTicket(ticketID, project string) (*store.WorkItem, string, error) {
	// Check if we already have a work item for this ticket
	existing, err := d.store.GetWorkItemByTicket(ticketID)
	if err == nil && existing != nil {
		ctx := fmt.Sprintf("## Ticket: %s\n**Title:** %s\n", ticketID, existing.Subject)
		if existing.Description != "" {
			ctx += fmt.Sprintf("**Description:** %s\n", existing.Description)
		}
		return existing, ctx, nil
	}

	// Try PM plugins to fetch ticket details
	var issue *pm.Issue
	var provider pm.Provider
	pmPlugins := d.pluginRegistry().GetEnabledByCategory(plugin.CategoryPM)
	for _, p := range pmPlugins {
		prov, ok := p.(pm.Provider)
		if !ok {
			continue
		}
		fetched, err := prov.GetIssue(context.Background(), ticketID)
		if err == nil && fetched != nil {
			issue = fetched
			provider = prov
			break
		}
	}

	ticketContext := buildTicketContext(ticketID, issue)

	// Route based on issue type
	switch {
	case issue != nil && issue.Type == pm.IssueTypeEpic:
		return d.resolveEpicTicket(issue, provider, project, ticketContext)
	case issue != nil && (issue.Type == pm.IssueTypeStory || issue.Type == pm.IssueTypeTask || issue.Type == pm.IssueTypeBug):
		return d.resolveStoryTicket(issue, provider, project, ticketContext)
	default:
		return d.resolveUnknownTicket(ticketID, issue, project, ticketContext)
	}
}

// resolveEpicTicket creates a Goal from an epic and Features from its children.
func (d *Daemon) resolveEpicTicket(issue *pm.Issue, provider pm.Provider, project, ticketContext string) (*store.WorkItem, string, error) {
	goalID, err := d.store.GenerateWorkItemID("")
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate work item ID: %w", err)
	}

	ticketRef := issue.Key
	goal := &store.WorkItem{
		ID:          goalID,
		Project:     project,
		ItemType:    store.WorkItemTypeGoal,
		Subject:     issue.Title,
		Description: issue.Description,
		Status:      store.WorkItemStatusInProgress,
		TicketID:    &ticketRef,
	}

	if err := d.store.CreateWorkItem(goal); err != nil {
		return nil, "", fmt.Errorf("failed to create goal: %w", err)
	}

	// Create features from children if available
	if provider != nil && len(issue.Children) > 0 {
		for _, childKey := range issue.Children {
			childIssue, err := provider.GetIssue(context.Background(), childKey)
			if err != nil {
				logging.Info("skipping child issue", "key", childKey, "error", err)
				continue
			}

			featureID, err := d.store.GenerateWorkItemID(goalID)
			if err != nil {
				logging.Info("skipping child issue", "key", childKey, "error", err)
				continue
			}

			childRef := childKey
			feature := &store.WorkItem{
				ID:          featureID,
				Project:     project,
				ItemType:    store.WorkItemTypeFeature,
				ParentID:    &goalID,
				Subject:     childIssue.Title,
				Description: childIssue.Description,
				Status:      store.WorkItemStatusPending,
				TicketID:    &childRef,
			}
			if err := d.store.CreateWorkItem(feature); err != nil {
				logging.Info("failed to create feature from child", "key", childKey, "error", err)
			}
		}
	}

	logging.Info("created goal from epic", "ticket", issue.Key, "goal", goalID, "children", len(issue.Children))
	return goal, ticketContext, nil
}

// resolveStoryTicket creates a Feature, optionally under a parent Goal from the epic.
func (d *Daemon) resolveStoryTicket(issue *pm.Issue, provider pm.Provider, project, ticketContext string) (*store.WorkItem, string, error) {
	var parentGoalID *string

	// If the story has a parent epic, look up or create the Goal
	if issue.ParentKey != "" && provider != nil {
		parentGoal, err := d.store.GetWorkItemByTicket(issue.ParentKey)
		if err == nil && parentGoal != nil {
			parentGoalID = &parentGoal.ID
		} else {
			// Create a goal from the parent epic
			parentIssue, err := provider.GetIssue(context.Background(), issue.ParentKey)
			if err == nil && parentIssue != nil {
				goalID, err := d.store.GenerateWorkItemID("")
				if err == nil {
					parentRef := issue.ParentKey
					goal := &store.WorkItem{
						ID:          goalID,
						Project:     project,
						ItemType:    store.WorkItemTypeGoal,
						Subject:     parentIssue.Title,
						Description: parentIssue.Description,
						Status:      store.WorkItemStatusInProgress,
						TicketID:    &parentRef,
					}
					if err := d.store.CreateWorkItem(goal); err == nil {
						parentGoalID = &goalID
						logging.Info("created parent goal from epic", "ticket", issue.ParentKey, "goal", goalID)
					}
				}
			}
		}
	}

	featureID, err := d.store.GenerateWorkItemID("")
	if parentGoalID != nil {
		featureID, err = d.store.GenerateWorkItemID(*parentGoalID)
	}
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate work item ID: %w", err)
	}

	ticketRef := issue.Key
	feature := &store.WorkItem{
		ID:          featureID,
		Project:     project,
		ItemType:    store.WorkItemTypeFeature,
		ParentID:    parentGoalID,
		Subject:     issue.Title,
		Description: issue.Description,
		Status:      store.WorkItemStatusInProgress,
		TicketID:    &ticketRef,
	}

	if err := d.store.CreateWorkItem(feature); err != nil {
		return nil, "", fmt.Errorf("failed to create feature: %w", err)
	}

	logging.Info("created feature from story", "ticket", issue.Key, "feature", featureID, "parent_goal", parentGoalID)
	return feature, ticketContext, nil
}

// resolveUnknownTicket falls back to creating a Goal (original behavior).
func (d *Daemon) resolveUnknownTicket(ticketID string, issue *pm.Issue, project, ticketContext string) (*store.WorkItem, string, error) {
	subject := ticketID
	description := ""
	if issue != nil {
		subject = issue.Title
		description = issue.Description
	}

	id, err := d.store.GenerateWorkItemID("")
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate work item ID: %w", err)
	}

	ticketRef := ticketID
	workItem := &store.WorkItem{
		ID:          id,
		Project:     project,
		ItemType:    store.WorkItemTypeGoal,
		Subject:     subject,
		Description: description,
		Status:      store.WorkItemStatusInProgress,
		TicketID:    &ticketRef,
	}

	if err := d.store.CreateWorkItem(workItem); err != nil {
		return nil, "", fmt.Errorf("failed to create work item: %w", err)
	}

	logging.Info("created goal work item from ticket", "ticket", ticketID, "work_item", id)
	return workItem, ticketContext, nil
}

// buildTicketContext builds the markdown context string for a ticket.
func buildTicketContext(ticketID string, issue *pm.Issue) string {
	if issue == nil {
		return fmt.Sprintf("## Ticket: %s\n", ticketID)
	}

	ctx := fmt.Sprintf("## Ticket: %s\n**Title:** %s\n", issue.Key, issue.Title)
	if issue.Type != pm.IssueTypeUnknown {
		ctx += fmt.Sprintf("**Type:** %s\n", issue.Type)
	}
	if issue.Description != "" {
		ctx += fmt.Sprintf("**Description:** %s\n", issue.Description)
	}
	if issue.Priority > 0 {
		ctx += fmt.Sprintf("**Priority:** %d\n", issue.Priority)
	}
	if len(issue.Labels) > 0 {
		ctx += fmt.Sprintf("**Labels:** %s\n", strings.Join(issue.Labels, ", "))
	}
	if issue.ParentKey != "" {
		ctx += fmt.Sprintf("**Parent:** %s\n", issue.ParentKey)
	}
	if len(issue.Children) > 0 {
		ctx += fmt.Sprintf("**Children:** %s\n", strings.Join(issue.Children, ", "))
	}
	return ctx
}

// buildSpawnPrompt constructs the system prompt injected via --append-system-prompt.
// This is the main integration point - it tells Claude about the Athena context,
// work item tracking, and what commands are available.
func (d *Daemon) buildSpawnPrompt(workItem *store.WorkItem, parentGoal *store.WorkItem, ticketContext, taskListID string, retrieve bool) string {
	var b strings.Builder

	// Header
	b.WriteString("# Athena Context\n\n")
	b.WriteString("You are working within Athena, an engineering orchestration system.\n")
	b.WriteString(fmt.Sprintf("Project: %s\n\n", workItem.Project))

	// Goal context (when spawning on a feature)
	if parentGoal != nil {
		b.WriteString("## Goal\n\n")
		b.WriteString(fmt.Sprintf("**Goal:** `%s` - %s\n", parentGoal.ID, parentGoal.Subject))
		if parentGoal.Description != "" {
			b.WriteString(fmt.Sprintf("**Description:** %s\n", parentGoal.Description))
		}
		b.WriteString("\n")
	}

	// Feature/work item context
	if workItem.ItemType == store.WorkItemTypeFeature {
		b.WriteString("## Feature\n\n")
		b.WriteString(fmt.Sprintf("**Feature:** `%s` - %s\n", workItem.ID, workItem.Subject))
		if workItem.Description != "" {
			b.WriteString(fmt.Sprintf("**Description:** %s\n", workItem.Description))
		}
		if workItem.WorktreePath != nil && *workItem.WorktreePath != "" {
			b.WriteString(fmt.Sprintf("**Worktree:** %s\n", *workItem.WorktreePath))
		}
		b.WriteString("\n")
	} else {
		// Non-feature work items
		b.WriteString(fmt.Sprintf("Work item: `%s` (%s)\n", workItem.ID, workItem.ItemType))
		if workItem.Subject != "" && workItem.Subject != "Interactive session" {
			b.WriteString(fmt.Sprintf("**Subject:** %s\n", workItem.Subject))
		}
		if workItem.Description != "" {
			b.WriteString(fmt.Sprintf("**Description:** %s\n", workItem.Description))
		}
		b.WriteString("\n")
	}

	// Ticket context if present
	if ticketContext != "" {
		b.WriteString(ticketContext)
		b.WriteString("\n")
	}

	// Task tracking instructions
	b.WriteString("## Task Tracking\n\n")
	b.WriteString(fmt.Sprintf("Your task list ID is `%s`. Use Claude Code's task tools (TaskCreate, TaskUpdate, TaskList) to:\n", taskListID))
	b.WriteString("- Break your work into trackable tasks before starting\n")
	b.WriteString("- Mark tasks `in_progress` when you start them\n")
	b.WriteString("- Mark tasks `completed` when done\n\n")

	// Mode-specific instructions
	if retrieve {
		b.WriteString("## Mode: Retrieve & Plan\n\n")
		b.WriteString("Before implementing, analyze this goal and decompose it:\n")
		b.WriteString("1. Explore the codebase to understand architecture and patterns\n")
		b.WriteString("2. Break the goal into discrete features/tasks using TaskCreate\n")
		b.WriteString("3. For each task, describe what needs to change and why\n")
		b.WriteString("4. Then implement each task in order, updating status as you go\n\n")
	}

	// Available Athena CLI commands
	b.WriteString("## Available Commands\n\n")
	b.WriteString("You have access to Athena CLI commands via `ath`:\n")
	b.WriteString("- `ath tree` - view work item hierarchy\n")
	b.WriteString("- `ath queue add` - add completed work to the merge queue\n")
	b.WriteString("- `ath wt` - list worktrees\n\n")

	// Completion instructions
	b.WriteString("## When Done\n\n")
	b.WriteString("When your work is complete:\n")
	b.WriteString("1. Ensure all tasks are marked `completed`\n")
	b.WriteString("2. Commit your changes with a clear message\n")
	b.WriteString("3. Run `ath queue add` to add this feature to the merge queue\n")

	return b.String()
}

// buildInteractiveExec constructs the exec args and env for interactive mode.
func (d *Daemon) buildInteractiveExec(prompt, archetype, taskListID string) ([]string, []string) {
	args := []string{"claude"}

	if archetypeCfg, ok := d.config.Archetypes[archetype]; ok {
		if archetypeCfg.Model != "" {
			args = append(args, "--model", archetypeCfg.Model)
		}
		if archetypeCfg.PermissionMode != "" {
			args = append(args, "--permission-mode", archetypeCfg.PermissionMode)
		}
	}

	args = append(args, "--append-system-prompt", prompt)

	env := []string{
		fmt.Sprintf("CLAUDE_CODE_TASK_LIST_ID=%s", taskListID),
	}

	return args, env
}

// pluginRegistry returns the daemon's plugin registry.
// TODO: Use the daemon's actual plugin registry instead of creating an empty one.
// This is a placeholder until plugin lifecycle management is implemented.
func (d *Daemon) pluginRegistry() *plugin.Registry {
	return plugin.NewRegistry()
}
