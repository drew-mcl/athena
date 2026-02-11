package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WorkflowContext stores the most recently created work item IDs
// to enable auto-linking workflow commands (goal -> feat -> spawn).
type WorkflowContext struct {
	LastGoalID    string `json:"last_goal_id,omitempty"`
	LastFeatureID string `json:"last_feature_id,omitempty"`
	LastTaskID    string `json:"last_task_id,omitempty"`
	Project       string `json:"project,omitempty"` // Project scope for context
}

// getContextPath returns the path to the workflow context file.
func getContextPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	athenaDir := filepath.Join(home, ".athena")
	if err := os.MkdirAll(athenaDir, 0755); err != nil {
		return "", fmt.Errorf("cannot create .athena directory: %w", err)
	}
	return filepath.Join(athenaDir, "context.json"), nil
}

// LoadContext loads the workflow context from disk.
// Returns an empty context if the file doesn't exist.
func LoadContext() (*WorkflowContext, error) {
	path, err := getContextPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No context file yet - return empty context
			return &WorkflowContext{}, nil
		}
		return nil, fmt.Errorf("failed to read context file: %w", err)
	}

	var ctx WorkflowContext
	if err := json.Unmarshal(data, &ctx); err != nil {
		return nil, fmt.Errorf("failed to parse context file: %w", err)
	}

	return &ctx, nil
}

// SaveContext saves the workflow context to disk.
func SaveContext(ctx *WorkflowContext) error {
	path, err := getContextPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(ctx, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal context: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write context file: %w", err)
	}

	return nil
}

// UpdateGoalContext updates the context with a newly created goal.
func UpdateGoalContext(goalID, project string) error {
	ctx, err := LoadContext()
	if err != nil {
		// Non-fatal - log but don't fail the command
		return err
	}

	ctx.LastGoalID = goalID
	ctx.Project = project

	return SaveContext(ctx)
}

// UpdateFeatureContext updates the context with a newly created feature.
func UpdateFeatureContext(featureID, project string) error {
	ctx, err := LoadContext()
	if err != nil {
		return err
	}

	ctx.LastFeatureID = featureID
	ctx.Project = project

	return SaveContext(ctx)
}

// UpdateTaskContext updates the context with a newly created task.
func UpdateTaskContext(taskID, project string) error {
	ctx, err := LoadContext()
	if err != nil {
		return err
	}

	ctx.LastTaskID = taskID
	ctx.Project = project

	return SaveContext(ctx)
}

// ClearContext removes the workflow context file.
func ClearContext() error {
	path, err := getContextPath()
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove context file: %w", err)
	}

	return nil
}
