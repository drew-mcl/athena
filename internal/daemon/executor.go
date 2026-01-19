// Package daemon implements the athenad background service.
package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/drewfead/athena/internal/agent"
	"github.com/drewfead/athena/internal/control"
	"github.com/drewfead/athena/internal/executil"
	"github.com/drewfead/athena/internal/logging"
	"github.com/drewfead/athena/internal/store"
	"github.com/drewfead/athena/internal/worktree"
)

// JobExecutor handles execution of different job types.
type JobExecutor struct {
	daemon *Daemon
}

// NewJobExecutor creates a new job executor.
func NewJobExecutor(d *Daemon) *JobExecutor {
	return &JobExecutor{daemon: d}
}

// ExecuteJob runs a job based on its type.
func (e *JobExecutor) ExecuteJob(ctx context.Context, job *store.Job) error {
	switch job.Type {
	case store.JobTypeQuestion:
		return e.executeQuestion(ctx, job)
	case store.JobTypeQuick:
		return e.executeQuick(ctx, job)
	case store.JobTypeFeature:
		return e.executeFeature(ctx, job)
	case store.JobTypeMerge:
		return e.executeMerge(ctx, job)
	default:
		return fmt.Errorf("unknown job type: %s", job.Type)
	}
}

// executeQuestion runs a simple Q&A job (no worktree needed).
func (e *JobExecutor) executeQuestion(ctx context.Context, job *store.Job) error {
	logging.Info("executing question job", "job_id", job.ID, "input", truncateStr(job.NormalizedInput, 50))

	e.updateJobStatus(job.ID, store.JobStatusExecuting)

	// Find any worktree for this project to run Claude in context
	worktrees, _ := e.daemon.store.ListWorktrees(job.Project)
	var workDir string
	for _, wt := range worktrees {
		if wt.IsMain {
			workDir = wt.Path
			break
		}
	}
	if workDir == "" && len(worktrees) > 0 {
		workDir = worktrees[0].Path
	}

	// Run Claude with the question (single turn, no file writes)
	answer, err := e.runClaudeQuestion(ctx, workDir, job.NormalizedInput)
	if err != nil {
		e.updateJobStatus(job.ID, store.JobStatusFailed)
		return fmt.Errorf("claude failed: %w", err)
	}

	// Store the answer
	e.daemon.store.UpdateJobAnswer(job.ID, answer)

	e.broadcast("job_completed", job.ID)
	return nil
}

// executeQuick runs a quick job: temp worktree → change → commit → broadcast to all worktrees.
func (e *JobExecutor) executeQuick(ctx context.Context, job *store.Job) error {
	logging.Info("executing quick job", "job_id", job.ID, "input", truncateStr(job.NormalizedInput, 50))

	e.updateJobStatus(job.ID, store.JobStatusExecuting)

	// 1. Find main repo for project
	mainRepo, err := e.findMainRepo(job.Project)
	if err != nil {
		e.updateJobStatus(job.ID, store.JobStatusFailed)
		return err
	}
	if err := e.ensureCleanRepo(mainRepo); err != nil {
		e.updateJobStatus(job.ID, store.JobStatusFailed)
		return err
	}

	// 2. Get default branch
	defaultBranch := e.getDefaultBranch(mainRepo)
	if job.TargetBranch != nil && *job.TargetBranch != "" {
		defaultBranch = *job.TargetBranch
	}

	// 3. Create temp worktree for the quick change
	tempWtName := fmt.Sprintf("%s-quick-%s", job.Project, job.ID[:8])
	tempWtPath := filepath.Join(filepath.Dir(mainRepo), tempWtName)
	tempBranch := fmt.Sprintf("quick/%s", job.ID[:8])

	if err := e.createTempWorktree(mainRepo, tempWtPath, tempBranch, defaultBranch); err != nil {
		e.updateJobStatus(job.ID, store.JobStatusFailed)
		return fmt.Errorf("failed to create temp worktree: %w", err)
	}
	e.daemon.store.UpdateJobWorktree(job.ID, tempWtPath)

	// 4. Run Claude to make the change
	if err := e.runClaudeQuickChange(ctx, tempWtPath, job.NormalizedInput); err != nil {
		e.cleanupTempWorktree(mainRepo, tempWtPath, tempBranch)
		e.updateJobStatus(job.ID, store.JobStatusFailed)
		return fmt.Errorf("claude failed: %w", err)
	}

	// 5. Check if any changes were made
	hasChanges, err := e.hasUncommittedChanges(tempWtPath)
	if err != nil || !hasChanges {
		e.cleanupTempWorktree(mainRepo, tempWtPath, tempBranch)
		if !hasChanges {
			e.updateJobStatus(job.ID, store.JobStatusCompleted)
			return nil // No changes needed
		}
		e.updateJobStatus(job.ID, store.JobStatusFailed)
		return err
	}

	// Safety check: Don't auto-commit massive changes
	stats, err := e.getChangeStats(tempWtPath)
	if err == nil {
		if stats.Files > 50 || stats.Insertions > 1000 || stats.Deletions > 1000 {
			logging.Warn("quick job produced massive changes - aborting auto-merge",
				"job_id", job.ID, "files", stats.Files, "insertions", stats.Insertions)
			// Don't cleanup - leave for review? Or maybe just fail for now to be safe.
			// Actually, let's leave the worktree but fail the job so user notices.
			e.updateJobStatus(job.ID, store.JobStatusFailed)
			return fmt.Errorf("safety limit exceeded: too many changes (%d files, +%d/-%d)",
				stats.Files, stats.Insertions, stats.Deletions)
		}
	}

	// 6. Commit the changes
	commitHash, err := e.commitChanges(tempWtPath, job.NormalizedInput)
	if err != nil {
		e.cleanupTempWorktree(mainRepo, tempWtPath, tempBranch)
		e.updateJobStatus(job.ID, store.JobStatusFailed)
		return fmt.Errorf("failed to commit: %w", err)
	}
	e.daemon.store.UpdateJobCommit(job.ID, commitHash)

	// 7. Merge to default branch
	if err := e.mergeToDefault(mainRepo, tempBranch, defaultBranch); err != nil {
		e.cleanupTempWorktree(mainRepo, tempWtPath, tempBranch)
		e.updateJobStatus(job.ID, store.JobStatusFailed)
		return fmt.Errorf("failed to merge to %s: %w", defaultBranch, err)
	}

	// 8. Cleanup temp worktree
	e.cleanupTempWorktree(mainRepo, tempWtPath, tempBranch)

	// 9. Broadcast to all feature worktrees
	results := e.broadcastToWorktrees(ctx, job, mainRepo, defaultBranch, commitHash)
	e.daemon.store.UpdateJobPropagation(job.ID, results)

	// 10. Determine final status based on propagation results
	hasConflicts := false
	for _, r := range results {
		if r.Status == "conflict" {
			hasConflicts = true
			break
		}
	}

	if hasConflicts {
		e.updateJobStatus(job.ID, store.JobStatusConflict)
	} else {
		e.updateJobStatus(job.ID, store.JobStatusCompleted)
	}

	e.broadcast("job_completed", job.ID)
	return nil
}

// executeFeature spawns a long-lived agent for feature work.
func (e *JobExecutor) executeFeature(ctx context.Context, job *store.Job) error {
	logging.Info("executing feature job", "job_id", job.ID, "input", truncateStr(job.NormalizedInput, 50))

	e.updateJobStatus(job.ID, store.JobStatusExecuting)

	// 1. Find main repo
	mainRepo, err := e.findMainRepo(job.Project)
	if err != nil {
		e.updateJobStatus(job.ID, store.JobStatusFailed)
		return err
	}

	// 2. Create a persistent worktree for this feature
	// Generate branch name from input + ID
	slug := sanitizeBranchName(job.NormalizedInput)
	if len(slug) > 30 {
		slug = slug[:30]
	}
	branch := fmt.Sprintf("feature/%s-%s", slug, job.ID[:6])

	// Create worktree via daemon's migrator (which handles directory structure)
	opts := worktree.CreateWorktreeOptions{
		MainRepoPath: mainRepo,
		Branch:       branch,
		Description:  job.NormalizedInput,
		WorkflowMode: string(e.daemon.config.UI.WorkflowMode),
	}

	wtPath, err := e.daemon.migrator.CreateWorktree(opts)
	if err != nil {
		e.updateJobStatus(job.ID, store.JobStatusFailed)
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	// Update job with worktree info
	e.daemon.store.UpdateJobWorktree(job.ID, wtPath)

	// 3. Spawn a planning agent
	planPrompt := fmt.Sprintf(`You are a planning agent. Analyze the following feature request and create a detailed implementation plan.

Feature Request: %s

Instructions:
1. Explore the codebase to understand the architecture and patterns
2. Identify the files that need to be modified or created
3. Use the EnterPlanMode tool to create your plan

IMPORTANT: Start your plan with YAML frontmatter containing a brief summary:
---
summary: One sentence describing what will be implemented
---

Then write the full plan with:
- Overview of the feature
- Step-by-step implementation plan
- Files to modify/create
- Testing considerations
- Potential risks or edge cases

Do NOT make any code changes. Only explore and create the plan.`, job.NormalizedInput)

	spec := agent.SpawnSpec{
		WorktreePath: wtPath,
		ProjectName:  job.Project,
		Archetype:    "planner",
		Prompt:       planPrompt,
	}

	spawnedAgent, err := e.daemon.spawner.Spawn(ctx, spec)
	if err != nil {
		logging.Warn("failed to spawn planning agent", "worktree", wtPath, "error", err)
		e.updateJobStatus(job.ID, store.JobStatusFailed)
		return fmt.Errorf("failed to spawn agent: %w", err)
	}

	// 4. Link everything up
	e.daemon.store.AssignAgentToWorktree(wtPath, spawnedAgent.ID)
	e.daemon.store.UpdateJobAgent(job.ID, spawnedAgent.ID)

	// Broadcast events
	e.daemon.server.Broadcast(control.Event{
		Type: "worktree_created",
		Payload: &control.WorktreeInfo{
			Path:     wtPath,
			Project:  job.Project,
			Branch:   branch,
			WTStatus: "active",
			AgentID:  spawnedAgent.ID,
		},
	})

	e.daemon.server.Broadcast(control.Event{
		Type:    "agent_created",
		Payload: e.daemon.agentToInfo(spawnedAgent),
	})

	e.updateJobStatus(job.ID, store.JobStatusPlanning)
	return nil
}

// executeMerge runs a resolver agent to fix merge conflicts.
func (e *JobExecutor) executeMerge(ctx context.Context, job *store.Job) error {
	logging.Info("executing merge resolution job", "job_id", job.ID)
	e.updateJobStatus(job.ID, store.JobStatusExecuting)

	// Merge jobs are typically created with a WorktreePath already associated or inferred
	var wtPath string
	if job.WorktreePath != nil {
		wtPath = *job.WorktreePath
	}
	if wtPath == "" {
		// Try to find worktree by project if not set (unlikely for merge jobs)
		return fmt.Errorf("no worktree path specified for merge job")
	}

	// Verify worktree exists
	wt, err := e.daemon.store.GetWorktree(wtPath)
	if err != nil || wt == nil {
		e.updateJobStatus(job.ID, store.JobStatusFailed)
		return fmt.Errorf("worktree not found: %s", wtPath)
	}

	// Spawn resolver agent
	prompt := fmt.Sprintf(`You are a merge conflict resolver. 
The current branch '%s' has merge conflicts with the default branch.

Instructions:
1. Run 'git status' to see conflicted files.
2. For each conflicted file, read the content and resolve the conflict markers (<<<<<<<, =======, >>>>>>>).
3. Ensure the code is syntactically correct and preserves the intent of both changes.
4. Run tests to verify the resolution if possible.
5. Use 'git add' to mark files as resolved.
6. Commit the merge with 'git commit'.

Feature Context: %s`, wt.Branch, job.NormalizedInput)

	spec := agent.SpawnSpec{
		WorktreePath: wtPath,
		ProjectName:  wt.Project,
		Archetype:    "resolver",
		Prompt:       prompt,
	}

	spawnedAgent, err := e.daemon.spawner.Spawn(ctx, spec)
	if err != nil {
		e.updateJobStatus(job.ID, store.JobStatusFailed)
		return fmt.Errorf("failed to spawn resolver: %w", err)
	}

	// Link agent
	e.daemon.store.AssignAgentToWorktree(wtPath, spawnedAgent.ID)
	e.daemon.store.UpdateJobAgent(job.ID, spawnedAgent.ID)

	e.daemon.server.Broadcast(control.Event{
		Type:    "agent_created",
		Payload: e.daemon.agentToInfo(spawnedAgent),
	})

	// Job status stays Executing until agent finishes?
	// Or we can mark it Planning/Running to match agent status.
	// For now, leave as Executing, the agent lifecycle will take over.
	return nil
}

// broadcastToWorktrees propagates changes to all feature worktrees.
func (e *JobExecutor) broadcastToWorktrees(ctx context.Context, job *store.Job, mainRepo, defaultBranch, commitHash string) []store.PropagationResult {
	worktrees, _ := e.daemon.store.ListWorktrees(job.Project)

	var results []store.PropagationResult
	for _, wt := range worktrees {
		if wt.IsMain {
			continue // Skip main repo
		}

		result := store.PropagationResult{
			WorktreePath: wt.Path,
			Branch:       wt.Branch,
		}

		// Check if worktree has an active agent
		if wt.AgentID != nil {
			agent, _ := e.daemon.store.GetAgent(*wt.AgentID)
			if agent != nil && isAgentActive(agent.Status) {
				// Notify agent instead of auto-merging
				result.Status = "notified"
				result.AgentID = *wt.AgentID
				e.notifyAgentToMerge(agent, job, commitHash)
				results = append(results, result)
				continue
			}
		}

		// No active agent - try auto-merge
		err := e.tryAutoMerge(wt.Path, defaultBranch)
		if err != nil {
			result.Status = "conflict"
			result.Error = err.Error()
			// Abort the failed merge
			e.abortMerge(wt.Path)
		} else {
			result.Status = "merged"
		}
		results = append(results, result)
	}

	return results
}

// notifyAgentToMerge sends a message to an agent to merge the latest changes.
func (e *JobExecutor) notifyAgentToMerge(agent *store.Agent, job *store.Job, commitHash string) {
	// Create an event that the agent can receive
	payload := fmt.Sprintf(`{"commit": "%s", "job_id": "%s", "message": "New changes on main: %s. Please merge when appropriate."}`,
		commitHash, job.ID, truncateStr(job.NormalizedInput, 100))
	e.daemon.store.LogAgentEvent(agent.ID, "merge_request", payload)

	// Broadcast to TUI
	e.daemon.server.Broadcast(control.Event{
		Type: "agent_merge_request",
		Payload: map[string]string{
			"agent_id": agent.ID,
			"job_id":   job.ID,
			"commit":   commitHash,
		},
	})

	logging.Info("notified agent to merge", "agent_id", agent.ID[:8], "commit", commitHash[:8])
}

// Helper methods

func (e *JobExecutor) updateJobStatus(id string, status store.JobStatus) {
	e.daemon.store.UpdateJobStatus(id, status)
	e.daemon.server.Broadcast(control.Event{
		Type:    "job_status_changed",
		Payload: map[string]string{"id": id, "status": string(status)},
	})
}

func (e *JobExecutor) broadcast(eventType, jobID string) {
	e.daemon.server.Broadcast(control.Event{
		Type:    eventType,
		Payload: map[string]string{"id": jobID},
	})
}

func (e *JobExecutor) findMainRepo(project string) (string, error) {
	worktrees, err := e.daemon.store.ListWorktrees(project)
	if err != nil {
		return "", err
	}
	for _, wt := range worktrees {
		if wt.IsMain {
			return wt.Path, nil
		}
	}
	return "", fmt.Errorf("no main repo found for project: %s", project)
}

func (e *JobExecutor) getDefaultBranch(repoPath string) string {
	cmd, err := executil.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD", "--short")
	if err == nil {
		cmd.Dir = repoPath
		output, err := cmd.Output()
		if err == nil {
			branch := strings.TrimSpace(string(output))
			return strings.TrimPrefix(branch, "origin/")
		}
	}

	// Fallback
	for _, branch := range []string{"main", "master"} {
		cmd, err = executil.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
		if err != nil {
			continue
		}
		cmd.Dir = repoPath
		if cmd.Run() == nil {
			return branch
		}
	}
	return "main"
}

func (e *JobExecutor) createTempWorktree(mainRepo, wtPath, branch, baseBranch string) error {
	// Create worktree with new branch from base
	cmd, err := executil.Command("git", "worktree", "add", "-b", branch, wtPath, baseBranch)
	if err != nil {
		return err
	}
	cmd.Dir = mainRepo
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", string(output), err)
	}
	return nil
}

func (e *JobExecutor) cleanupTempWorktree(mainRepo, wtPath, branch string) {
	// Remove worktree
	if cmd, err := executil.Command("git", "worktree", "remove", "--force", wtPath); err == nil {
		cmd.Dir = mainRepo
		cmd.Run()
	}

	// Delete branch
	if cmd, err := executil.Command("git", "branch", "-D", branch); err == nil {
		cmd.Dir = mainRepo
		cmd.Run()
	}
}

func (e *JobExecutor) runClaudeQuestion(ctx context.Context, workDir, question string) (string, error) {
	// Run Claude in print mode for a quick answer
	args := []string{
		"--print",
		"--output-format", "text",
		"--max-turns", "1",
		"--permission-mode", "plan", // Read-only
		question,
	}

	cmd, err := executil.CommandContext(ctx, "claude", args...)
	if err != nil {
		return "", err
	}
	if workDir != "" {
		cmd.Dir = workDir
	}

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (e *JobExecutor) runClaudeQuickChange(ctx context.Context, wtPath, task string) error {
	// Run Claude to make the quick change
	prompt := fmt.Sprintf(`Make this quick change and commit when done: %s

Important:
- Make minimal, focused changes
- This is a quick fix/update, not a feature
- Commit with a clear message when done`, task)

	args := []string{
		"--print",
		"--output-format", "text",
		"--max-turns", "5", // Limited turns for quick jobs
		prompt,
	}

	cmd, err := executil.CommandContext(ctx, "claude", args...)
	if err != nil {
		return err
	}
	cmd.Dir = wtPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		logging.Debug("claude quick change output", "output", string(output))
		return err
	}
	return nil
}

func (e *JobExecutor) hasUncommittedChanges(repoPath string) (bool, error) {
	cmd, err := executil.Command("git", "status", "--porcelain")
	if err != nil {
		return false, err
	}
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(output))) > 0, nil
}

func (e *JobExecutor) commitChanges(repoPath, message string) (string, error) {
	// Stage all changes
	cmd, err := executil.Command("git", "add", "-A")
	if err != nil {
		return "", err
	}
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git add failed: %w", err)
	}

	// Commit
	commitMsg := truncateStr(message, 72)
	cmd, err = executil.Command("git", "commit", "-m", commitMsg)
	if err != nil {
		return "", err
	}
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git commit failed: %w", err)
	}

	// Get commit hash
	cmd, err = executil.Command("git", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (e *JobExecutor) mergeToDefault(mainRepo, sourceBranch, targetBranch string) error {
	originalBranch, err := e.getCurrentBranch(mainRepo)
	if err != nil {
		return err
	}
	if originalBranch == "" {
		return fmt.Errorf("cannot merge: repository is in detached HEAD state: %s", mainRepo)
	}

	if err := e.ensureCleanRepo(mainRepo); err != nil {
		return err
	}

	if originalBranch != "" && originalBranch != targetBranch {
		if err := e.checkoutBranch(mainRepo, targetBranch); err != nil {
			return err
		}
	}

	mergeErr := e.mergeBranch(mainRepo, sourceBranch)
	if mergeErr != nil {
		e.abortMerge(mainRepo)
	}

	if originalBranch != "" && originalBranch != targetBranch {
		if err := e.checkoutBranch(mainRepo, originalBranch); err != nil {
			logging.Warn("failed to restore branch after merge", "repo", mainRepo, "branch", originalBranch, "error", err)
		}
	}

	return mergeErr
}

func (e *JobExecutor) tryAutoMerge(wtPath, defaultBranch string) error {
	// Fetch latest
	if cmd, err := executil.Command("git", "fetch", "origin", defaultBranch); err == nil {
		cmd.Dir = wtPath
		cmd.Run() // Ignore errors
	}

	// Try to merge
	cmd, err := executil.Command("git", "merge", "--no-edit", "origin/"+defaultBranch)
	if err != nil {
		return err
	}
	cmd.Dir = wtPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("merge conflict: %s", string(output))
	}
	return nil
}

func (e *JobExecutor) abortMerge(wtPath string) {
	if cmd, err := executil.Command("git", "merge", "--abort"); err == nil {
		cmd.Dir = wtPath
		cmd.Run()
	}
}

func (e *JobExecutor) ensureCleanRepo(repoPath string) error {
	dirty, err := e.hasUncommittedChanges(repoPath)
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("repository has uncommitted changes: %s", repoPath)
	}
	return nil
}

func (e *JobExecutor) getCurrentBranch(repoPath string) (string, error) {
	cmd, err := executil.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to read current branch: %w", err)
	}
	branch := strings.TrimSpace(string(output))
	if branch == "HEAD" {
		return "", nil
	}
	return branch, nil
}

func (e *JobExecutor) checkoutBranch(repoPath, branch string) error {
	cmd, err := executil.Command("git", "checkout", branch)
	if err != nil {
		return err
	}
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("checkout %s failed: %w", branch, err)
	}
	return nil
}

func (e *JobExecutor) mergeBranch(repoPath, branch string) error {
	cmd, err := executil.Command("git", "merge", "--no-edit", branch)
	if err != nil {
		return err
	}
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("merge failed: %s", string(output))
	}
	return nil
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func isAgentActive(status store.AgentStatus) bool {
	switch status {
	case store.AgentStatusRunning, store.AgentStatusPlanning,
		store.AgentStatusExecuting, store.AgentStatusSpawning,
		store.AgentStatusAttached:
		return true
	}
	return false
}

func sanitizeBranchName(input string) string {
	// Simple sanitizer: replace spaces/specials with dashes, lowercase
	s := strings.ToLower(input)
	var sb strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' {
			sb.WriteRune('-')
		}
	}
	return strings.Trim(sb.String(), "-")
}

type changeStats struct {
	Files      int
	Insertions int
	Deletions  int
}

func (e *JobExecutor) getChangeStats(repoPath string) (*changeStats, error) {
	cmd := exec.Command("git", "diff", "--shortstat", "HEAD")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	// Format: " 1 file changed, 2 insertions(+), 1 deletion(-)"
	s := strings.TrimSpace(string(output))
	var stats changeStats

	// Very basic parsing - could be more robust
	parts := strings.Split(s, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		var val int
		if _, err := fmt.Sscanf(part, "%d file", &val); err == nil {
			stats.Files = val
		} else if _, err := fmt.Sscanf(part, "%d insertion", &val); err == nil {
			stats.Insertions = val
		} else if _, err := fmt.Sscanf(part, "%d deletion", &val); err == nil {
			stats.Deletions = val
		}
	}
	return &stats, nil
}
