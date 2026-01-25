// Package claude provides a task provider for Claude Code's task storage.
package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/drewfead/athena/internal/logging"
	"github.com/drewfead/athena/internal/task"
	"github.com/fsnotify/fsnotify"
)

const (
	// ProviderName is the identifier for this provider.
	ProviderName = "claude"
)

// taskFile represents the structure of a Claude Code task list file.
type taskFile struct {
	Tasks []taskRecord `json:"tasks"`
}

// taskRecord represents a single task in Claude Code's storage format.
type taskRecord struct {
	ID          string         `json:"id"`
	Subject     string         `json:"subject"`
	Description string         `json:"description,omitempty"`
	Status      string         `json:"status"` // pending, in_progress, completed
	ActiveForm  string         `json:"activeForm,omitempty"`
	Owner       string         `json:"owner,omitempty"`
	Blocks      []string       `json:"blocks,omitempty"`
	BlockedBy   []string       `json:"blockedBy,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   string         `json:"createdAt,omitempty"`
	UpdatedAt   string         `json:"updatedAt,omitempty"`
}

// Provider implements the task.Provider interface for Claude Code.
type Provider struct {
	tasksDir string
	mu       sync.RWMutex
	cache    map[string]*taskFile // listID -> cached file contents
}

// NewProvider creates a new Claude Code task provider.
func NewProvider() (*Provider, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	tasksDir := filepath.Join(homeDir, ".claude", "tasks")

	// Create the tasks directory if it doesn't exist (restrictive permissions for security)
	if err := os.MkdirAll(tasksDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create tasks directory: %w", err)
	}

	return &Provider{
		tasksDir: tasksDir,
		cache:    make(map[string]*taskFile),
	}, nil
}

// NewProviderWithPath creates a provider with a custom tasks directory (for testing).
func NewProviderWithPath(tasksDir string) *Provider {
	return &Provider{
		tasksDir: tasksDir,
		cache:    make(map[string]*taskFile),
	}
}

// Name implements task.Provider.
func (p *Provider) Name() string {
	return ProviderName
}

// ListTaskLists implements task.Provider.
func (p *Provider) ListTaskLists() ([]task.TaskList, error) {
	entries, err := os.ReadDir(p.tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read tasks directory: %w", err)
	}

	var lists []task.TaskList
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		listID := entry.Name()[:len(entry.Name())-5] // Remove .json extension
		filePath := filepath.Join(p.tasksDir, entry.Name())

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Load the file to count tasks
		tasks, err := p.loadTaskFile(listID)
		if err != nil {
			continue
		}

		lists = append(lists, task.TaskList{
			ID:        listID,
			Name:      listID, // Use ID as name if no metadata
			Provider:  ProviderName,
			Path:      filePath,
			TaskCount: len(tasks.Tasks),
			CreatedAt: info.ModTime(), // Use file mod time as approximation
			UpdatedAt: info.ModTime(),
		})
	}

	return lists, nil
}

// ListTasks implements task.Provider.
func (p *Provider) ListTasks(listID string, filters task.TaskFilters) ([]task.Task, error) {
	tf, err := p.loadTaskFile(listID)
	if err != nil {
		return nil, err
	}

	var result []task.Task
	for _, tr := range tf.Tasks {
		t := p.recordToTask(listID, tr)

		// Apply filters
		if filters.Status != nil && t.Status != *filters.Status {
			continue
		}
		if filters.Owner != nil && t.Owner != *filters.Owner {
			continue
		}
		if filters.Blocked != nil {
			isBlocked := len(t.BlockedBy) > 0
			if *filters.Blocked != isBlocked {
				continue
			}
		}

		result = append(result, t)
	}

	return result, nil
}

// GetTask implements task.Provider.
func (p *Provider) GetTask(listID, taskID string) (*task.Task, error) {
	tf, err := p.loadTaskFile(listID)
	if err != nil {
		return nil, err
	}

	for _, tr := range tf.Tasks {
		if tr.ID == taskID {
			t := p.recordToTask(listID, tr)
			return &t, nil
		}
	}

	return nil, fmt.Errorf("task %s not found in list %s", taskID, listID)
}

// CreateTask implements task.Provider.
func (p *Provider) CreateTask(listID string, create *task.TaskCreate) (*task.Task, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	tf, err := p.loadTaskFileUnsafe(listID)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if tf == nil {
		tf = &taskFile{Tasks: []taskRecord{}}
	}

	now := time.Now()
	id := fmt.Sprintf("task-%d", now.UnixNano())

	status := create.Status
	if status == "" {
		status = task.StatusPending
	}

	tr := taskRecord{
		ID:          id,
		Subject:     create.Subject,
		Description: create.Description,
		Status:      string(status),
		ActiveForm:  create.ActiveForm,
		Blocks:      create.Blocks,
		BlockedBy:   create.BlockedBy,
		Metadata:    create.Metadata,
		CreatedAt:   now.Format(time.RFC3339),
		UpdatedAt:   now.Format(time.RFC3339),
	}

	tf.Tasks = append(tf.Tasks, tr)

	if err := p.saveTaskFile(listID, tf); err != nil {
		return nil, err
	}

	t := p.recordToTask(listID, tr)
	return &t, nil
}

// UpdateTask implements task.Provider.
func (p *Provider) UpdateTask(listID, taskID string, update *task.TaskUpdate) (*task.Task, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	tf, err := p.loadTaskFileUnsafe(listID)
	if err != nil {
		return nil, err
	}

	var found *taskRecord
	for i := range tf.Tasks {
		if tf.Tasks[i].ID == taskID {
			found = &tf.Tasks[i]
			break
		}
	}

	if found == nil {
		return nil, fmt.Errorf("task %s not found in list %s", taskID, listID)
	}

	// Apply updates
	if update.Subject != nil {
		found.Subject = *update.Subject
	}
	if update.Description != nil {
		found.Description = *update.Description
	}
	if update.Status != nil {
		found.Status = string(*update.Status)
	}
	if update.ActiveForm != nil {
		found.ActiveForm = *update.ActiveForm
	}
	if update.Owner != nil {
		found.Owner = *update.Owner
	}
	if len(update.AddBlocks) > 0 {
		found.Blocks = appendUnique(found.Blocks, update.AddBlocks...)
	}
	if len(update.AddBlockedBy) > 0 {
		found.BlockedBy = appendUnique(found.BlockedBy, update.AddBlockedBy...)
	}
	if update.Metadata != nil {
		if found.Metadata == nil {
			found.Metadata = make(map[string]any)
		}
		for k, v := range update.Metadata {
			if v == nil {
				delete(found.Metadata, k)
			} else {
				found.Metadata[k] = v
			}
		}
	}
	found.UpdatedAt = time.Now().Format(time.RFC3339)

	if err := p.saveTaskFile(listID, tf); err != nil {
		return nil, err
	}

	t := p.recordToTask(listID, *found)
	return &t, nil
}

// DeleteTask implements task.Provider.
func (p *Provider) DeleteTask(listID, taskID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	tf, err := p.loadTaskFileUnsafe(listID)
	if err != nil {
		return err
	}

	newTasks := make([]taskRecord, 0, len(tf.Tasks))
	found := false
	for _, t := range tf.Tasks {
		if t.ID == taskID {
			found = true
			continue
		}
		newTasks = append(newTasks, t)
	}

	if !found {
		return fmt.Errorf("task %s not found in list %s", taskID, listID)
	}

	tf.Tasks = newTasks
	return p.saveTaskFile(listID, tf)
}

// Watch implements task.Provider.
func (p *Provider) Watch(ctx context.Context) (<-chan task.TaskEvent, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}

	if err := watcher.Add(p.tasksDir); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("failed to watch tasks directory: %w", err)
	}

	events := make(chan task.TaskEvent, 100)

	go func() {
		defer watcher.Close()
		defer close(events)

		for {
			select {
			case <-ctx.Done():
				return

			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				// Only handle .json files
				if filepath.Ext(event.Name) != ".json" {
					continue
				}

				listID := filepath.Base(event.Name)
				listID = listID[:len(listID)-5] // Remove .json

				// Invalidate cache
				p.mu.Lock()
				delete(p.cache, listID)
				p.mu.Unlock()

				// Send a list sync event
				taskEvent := task.TaskEvent{
					Type:   task.EventTypeListSync,
					ListID: listID,
				}

				select {
				case events <- taskEvent:
				case <-ctx.Done():
					return
				}

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				logging.Warn("task watcher error", "error", err)
			}
		}
	}()

	return events, nil
}

// loadTaskFile loads and caches a task file.
func (p *Provider) loadTaskFile(listID string) (*taskFile, error) {
	p.mu.RLock()
	if cached, ok := p.cache[listID]; ok {
		p.mu.RUnlock()
		return cached, nil
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	return p.loadTaskFileUnsafe(listID)
}

// loadTaskFileUnsafe loads a task file without locking (caller must hold lock).
func (p *Provider) loadTaskFileUnsafe(listID string) (*taskFile, error) {
	// Check cache again in case another goroutine loaded it
	if cached, ok := p.cache[listID]; ok {
		return cached, nil
	}

	filePath := filepath.Join(p.tasksDir, listID+".json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var tf taskFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return nil, fmt.Errorf("failed to parse task file %s: %w", filePath, err)
	}

	p.cache[listID] = &tf
	return &tf, nil
}

// saveTaskFile writes a task file to disk and updates the cache.
func (p *Provider) saveTaskFile(listID string, tf *taskFile) error {
	filePath := filepath.Join(p.tasksDir, listID+".json")

	data, err := json.MarshalIndent(tf, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal task file: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write task file %s: %w", filePath, err)
	}

	p.cache[listID] = tf
	return nil
}

// recordToTask converts a storage record to a task.Task.
func (p *Provider) recordToTask(listID string, tr taskRecord) task.Task {
	createdAt, _ := time.Parse(time.RFC3339, tr.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339, tr.UpdatedAt)

	return task.Task{
		ID:          tr.ID,
		ListID:      listID,
		Subject:     tr.Subject,
		Description: tr.Description,
		Status:      task.Status(tr.Status),
		ActiveForm:  tr.ActiveForm,
		Owner:       tr.Owner,
		Blocks:      tr.Blocks,
		BlockedBy:   tr.BlockedBy,
		Metadata:    tr.Metadata,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
}

// appendUnique appends items to a slice, skipping duplicates.
func appendUnique(slice []string, items ...string) []string {
	seen := make(map[string]bool)
	for _, s := range slice {
		seen[s] = true
	}

	for _, item := range items {
		if !seen[item] {
			slice = append(slice, item)
			seen[item] = true
		}
	}
	return slice
}
