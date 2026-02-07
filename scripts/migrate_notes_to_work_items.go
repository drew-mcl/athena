//go:build ignore

// Script to migrate notes to hierarchical work items structure.
// Run with: go run scripts/migrate_notes_to_work_items.go
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Note struct {
	ID        string
	Content   string
	Done      bool
	CreatedAt time.Time
}

type WorkItem struct {
	ID          string
	Project     string
	ItemType    string
	ParentID    *string
	Subject     string
	Description string
	Status      string
	Priority    int
}

func main() {
	db, err := sql.Open("sqlite3", expandPath("~/.local/share/athena/athena.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Create goals
	goals := map[string]string{
		"wi-tui":   "TUI/UX Improvements - Better dashboard, agent views, status displays",
		"wi-auto":  "Workflow Automation - Auto workflows, merge trains, CI/CD integration",
		"wi-integ": "External Integrations - Sentry, Sonar, GitHub, Jira, Docker, K8s",
		"wi-docs":  "Documentation & Quality - Test coverage, docs, developer experience",
		"wi-orch":  "Orchestration - Multi-agent coordination, task distribution",
		"wi-infra": "Infrastructure - Release workflow, Homebrew, MCP servers",
		"wi-fixes": "Bug Fixes - Critical and high priority fixes from code review",
	}

	for id, subject := range goals {
		createWorkItem(db, id, "athena", "goal", nil, subject, "", "pending", 2)
	}

	// Feature groupings under goals
	features := map[string]struct {
		parent  string
		subject string
	}{
		// TUI features
		"wi-tui.1": {"wi-tui", "Agent view improvements - summary, metrics, tool use"},
		"wi-tui.2": {"wi-tui", "Worktree page enhancements - table view, status, CI"},
		"wi-tui.3": {"wi-tui", "Status colors and visual polish"},
		"wi-tui.4": {"wi-tui", "Plan viewing and approval workflow"},

		// Automation features
		"wi-auto.1": {"wi-auto", "Auto workflow mode - notes to PR pipeline"},
		"wi-auto.2": {"wi-auto", "Merge train and consolidation flow"},
		"wi-auto.3": {"wi-auto", "CI/CD integration and status display"},

		// Integration features
		"wi-integ.1": {"wi-integ", "Sentry integration - issues, monitoring"},
		"wi-integ.2": {"wi-integ", "SonarQube integration - code quality"},
		"wi-integ.3": {"wi-integ", "Project management - Jira/Linear sync"},
		"wi-integ.4": {"wi-integ", "Container/K8s plugin - ephemeral testing"},
		"wi-integ.5": {"wi-integ", "MCP servers management page"},

		// Docs/Quality features
		"wi-docs.1": {"wi-docs", "Test coverage improvements"},
		"wi-docs.2": {"wi-docs", "Documentation and CLAUDE.md"},
		"wi-docs.3": {"wi-docs", "Metrics and KPIs dashboard"},

		// Orchestration features
		"wi-orch.1": {"wi-orch", "Multi-agent worktree coordination"},
		"wi-orch.2": {"wi-orch", "Ralph/feedback loop integration"},
		"wi-orch.3": {"wi-orch", "Task templates and archetypes"},

		// Infrastructure features
		"wi-infra.1": {"wi-infra", "Release workflow with git-cliff"},
		"wi-infra.2": {"wi-infra", "Homebrew distribution"},
		"wi-infra.3": {"wi-infra", "LSP and Docker dev environment"},
	}

	for id, f := range features {
		createWorkItem(db, id, "athena", "feature", &f.parent, f.subject, "", "pending", 2)
	}

	// Now import open notes as tasks
	notes := getOpenNotes(db)
	fmt.Printf("Found %d open notes to categorize\n", len(notes))

	for _, note := range notes {
		featureID := categorizeNote(note.Content)
		taskID := fmt.Sprintf("%s.%d", featureID, getNextSeq(db, featureID))

		// Truncate subject to first 80 chars
		subject := note.Content
		if len(subject) > 80 {
			subject = subject[:77] + "..."
		}

		createWorkItem(db, taskID, "athena", "task", &featureID, subject, note.Content, "pending", 2)
		fmt.Printf("  -> %s: %s\n", taskID, subject[:min(40, len(subject))])
	}

	fmt.Println("\nMigration complete!")
	fmt.Println("Run 'ath tree' to see the hierarchy")
}

func categorizeNote(content string) string {
	lower := strings.ToLower(content)

	// Bug fixes (from code review)
	if strings.Contains(lower, "critical:") || strings.Contains(lower, "high:") ||
		strings.Contains(lower, "medium:") || strings.Contains(lower, "design:") {
		return "wi-fixes.1"
	}

	// TUI/UX
	if strings.Contains(lower, "agent view") || strings.Contains(lower, "adgent view") ||
		strings.Contains(lower, "ui") || strings.Contains(lower, "page") ||
		strings.Contains(lower, "display") || strings.Contains(lower, "colour") ||
		strings.Contains(lower, "color") || strings.Contains(lower, "status") {
		if strings.Contains(lower, "agent") || strings.Contains(lower, "adgent") {
			return "wi-tui.1"
		}
		if strings.Contains(lower, "worktree") || strings.Contains(lower, "wt") {
			return "wi-tui.2"
		}
		if strings.Contains(lower, "plan") {
			return "wi-tui.4"
		}
		return "wi-tui.3"
	}

	// Workflow automation
	if strings.Contains(lower, "workflow") || strings.Contains(lower, "auto") ||
		strings.Contains(lower, "merge") || strings.Contains(lower, "pr") ||
		strings.Contains(lower, "publish") {
		if strings.Contains(lower, "merge") || strings.Contains(lower, "train") {
			return "wi-auto.2"
		}
		if strings.Contains(lower, "cicd") || strings.Contains(lower, "ci/cd") {
			return "wi-auto.3"
		}
		return "wi-auto.1"
	}

	// Integrations
	if strings.Contains(lower, "sentry") {
		return "wi-integ.1"
	}
	if strings.Contains(lower, "sonar") {
		return "wi-integ.2"
	}
	if strings.Contains(lower, "jira") || strings.Contains(lower, "linear") ||
		strings.Contains(lower, "pm ") || strings.Contains(lower, "project management") {
		return "wi-integ.3"
	}
	if strings.Contains(lower, "docker") || strings.Contains(lower, "kubernetes") ||
		strings.Contains(lower, "k8s") {
		return "wi-integ.4"
	}
	if strings.Contains(lower, "mcp") || strings.Contains(lower, "plugin") {
		return "wi-integ.5"
	}

	// Documentation/Quality
	if strings.Contains(lower, "test") || strings.Contains(lower, "coverage") {
		return "wi-docs.1"
	}
	if strings.Contains(lower, "doc") || strings.Contains(lower, "claude.md") {
		return "wi-docs.2"
	}
	if strings.Contains(lower, "metric") || strings.Contains(lower, "kpi") {
		return "wi-docs.3"
	}

	// Orchestration
	if strings.Contains(lower, "orchestrat") || strings.Contains(lower, "multi-agent") ||
		strings.Contains(lower, "ralph") {
		return "wi-orch.1"
	}
	if strings.Contains(lower, "template") || strings.Contains(lower, "archetype") {
		return "wi-orch.3"
	}

	// Infrastructure
	if strings.Contains(lower, "release") || strings.Contains(lower, "gitcliff") ||
		strings.Contains(lower, "git-cliff") {
		return "wi-infra.1"
	}
	if strings.Contains(lower, "homebrew") || strings.Contains(lower, "run.sh") {
		return "wi-infra.2"
	}
	if strings.Contains(lower, "lsp") || strings.Contains(lower, "docker") {
		return "wi-infra.3"
	}

	// Default to TUI improvements (most common category)
	return "wi-tui.3"
}

func getOpenNotes(db *sql.DB) []Note {
	rows, err := db.Query(`SELECT id, content, done, created_at FROM notes WHERE done = 0 ORDER BY created_at DESC`)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var notes []Note
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.Content, &n.Done, &n.CreatedAt); err != nil {
			log.Fatal(err)
		}
		notes = append(notes, n)
	}
	return notes
}

func createWorkItem(db *sql.DB, id, project, itemType string, parentID *string, subject, description, status string, priority int) {
	// Check if exists
	var exists int
	db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE id = ?`, id).Scan(&exists)
	if exists > 0 {
		return
	}

	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO work_items (id, project, item_type, parent_id, subject, description, status, priority, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, project, itemType, parentID, subject, description, status, priority, now, now)
	if err != nil {
		log.Printf("Failed to create %s: %v", id, err)
	}
}

func getNextSeq(db *sql.DB, parentID string) int {
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id = ?`, parentID).Scan(&count)
	return count + 1
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return home + path[1:]
	}
	return path
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
