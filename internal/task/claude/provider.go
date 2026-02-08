// Package claude provides a task provider for Claude Code's task storage.
//
// Claude Code stores tasks in a directory-per-list format:
//
//	~/.claude/tasks/{listID}/1.json
//	~/.claude/tasks/{listID}/2.json
//	~/.claude/tasks/{listID}/.lock
//	~/.claude/tasks/{listID}/.highwatermark
//
// Each .json file is a single task object (not wrapped in an array).
// Task IDs are numeric, assigned from a highwatermark counter.
package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
	cache    map[string][]taskRecord // listID -> cached task records
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
		cache:    make(map[string][]taskRecord),
	}, nil
}

// NewProviderWithPath creates a provider with a custom tasks directory (for testing).
func NewProviderWithPath(tasksDir string) *Provider {
	return &Provider{
		tasksDir: tasksDir,
		cache:    make(map[string][]taskRecord),
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
		if !entry.IsDir() {
			continue
		}

		listID := entry.Name()
		listDir := filepath.Join(p.tasksDir, listID)

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Count .json task files in the directory
		taskCount := countTaskFiles(listDir)

		// .lock file means an agent is actively working on this list
		active := hasLockFile(listDir)

		lists = append(lists, task.TaskList{
			ID:        listID,
			Name:      listID,
			Provider:  ProviderName,
			Path:      listDir,
			TaskCount: taskCount,
			Active:    active,
			CreatedAt: info.ModTime(),
			UpdatedAt: info.ModTime(),
		})
	}

	return lists, nil
}

// ListTasks implements task.Provider.
func (p *Provider) ListTasks(listID string, filters task.TaskFilters) ([]task.Task, error) {
	records, err := p.loadTaskDir(listID)
	if err != nil {
		return nil, err
	}

	var result []task.Task
	for _, tr := range records {
		t := recordToTask(listID, tr)

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
	records, err := p.loadTaskDir(listID)
	if err != nil {
		return nil, err
	}

	for _, tr := range records {
		if tr.ID == taskID {
			t := recordToTask(listID, tr)
			return &t, nil
		}
	}

	return nil, fmt.Errorf("task %s not found in list %s", taskID, listID)
}

// CreateTask implements task.Provider.
func (p *Provider) CreateTask(listID string, create *task.TaskCreate) (*task.Task, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	listDir := filepath.Join(p.tasksDir, listID)

	// Ensure the list directory exists
	if err := os.MkdirAll(listDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create task list directory: %w", err)
	}

	// Determine next ID from highwatermark
	nextID := p.nextTaskID(listDir)

	now := time.Now()
	status := create.Status
	if status == "" {
		status = task.StatusPending
	}

	tr := taskRecord{
		ID:          strconv.Itoa(nextID),
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

	if err := writeTaskFile(listDir, tr); err != nil {
		return nil, err
	}

	// Update highwatermark
	writeHighwatermark(listDir, nextID)

	// Invalidate cache
	delete(p.cache, listID)

	t := recordToTask(listID, tr)
	return &t, nil
}

// UpdateTask implements task.Provider.
func (p *Provider) UpdateTask(listID, taskID string, update *task.TaskUpdate) (*task.Task, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	listDir := filepath.Join(p.tasksDir, listID)
	filePath := filepath.Join(listDir, taskID+".json")

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("task %s not found in list %s", taskID, listID)
		}
		return nil, fmt.Errorf("failed to read task file: %w", err)
	}

	var tr taskRecord
	if err := json.Unmarshal(data, &tr); err != nil {
		return nil, fmt.Errorf("failed to parse task file %s: %w", filePath, err)
	}

	// Apply updates
	if update.Subject != nil {
		tr.Subject = *update.Subject
	}
	if update.Description != nil {
		tr.Description = *update.Description
	}
	if update.Status != nil {
		tr.Status = string(*update.Status)
	}
	if update.ActiveForm != nil {
		tr.ActiveForm = *update.ActiveForm
	}
	if update.Owner != nil {
		tr.Owner = *update.Owner
	}
	if len(update.AddBlocks) > 0 {
		tr.Blocks = appendUnique(tr.Blocks, update.AddBlocks...)
	}
	if len(update.AddBlockedBy) > 0 {
		tr.BlockedBy = appendUnique(tr.BlockedBy, update.AddBlockedBy...)
	}
	if update.Metadata != nil {
		if tr.Metadata == nil {
			tr.Metadata = make(map[string]any)
		}
		for k, v := range update.Metadata {
			if v == nil {
				delete(tr.Metadata, k)
			} else {
				tr.Metadata[k] = v
			}
		}
	}
	tr.UpdatedAt = time.Now().Format(time.RFC3339)

	if err := writeTaskFile(listDir, tr); err != nil {
		return nil, err
	}

	// Invalidate cache
	delete(p.cache, listID)

	t := recordToTask(listID, tr)
	return &t, nil
}

// DeleteTask implements task.Provider.
func (p *Provider) DeleteTask(listID, taskID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	filePath := filepath.Join(p.tasksDir, listID, taskID+".json")

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("task %s not found in list %s", taskID, listID)
	}

	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to delete task file %s: %w", filePath, err)
	}

	// Invalidate cache
	delete(p.cache, listID)
	return nil
}

// Watch implements task.Provider.
func (p *Provider) Watch(ctx context.Context) (<-chan task.TaskEvent, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}

	// Watch the top-level tasks directory for new list directories
	if err := watcher.Add(p.tasksDir); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("failed to watch tasks directory: %w", err)
	}

	// Watch all existing list subdirectories for task file changes
	entries, err := os.ReadDir(p.tasksDir)
	if err != nil && !os.IsNotExist(err) {
		watcher.Close()
		return nil, fmt.Errorf("failed to read tasks directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			subDir := filepath.Join(p.tasksDir, entry.Name())
			if err := watcher.Add(subDir); err != nil {
				logging.Debug("failed to watch task list directory", "dir", subDir, "error", err)
			}
		}
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

				p.handleFSEvent(ctx, watcher, event, events)

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

// handleFSEvent processes a filesystem event and emits task events.
func (p *Provider) handleFSEvent(ctx context.Context, watcher *fsnotify.Watcher, event fsnotify.Event, events chan<- task.TaskEvent) {
	rel, err := filepath.Rel(p.tasksDir, event.Name)
	if err != nil {
		return
	}

	parts := strings.SplitN(rel, string(filepath.Separator), 2)

	switch len(parts) {
	case 1:
		// Event in top-level tasks dir — a new list directory appeared
		if event.Has(fsnotify.Create) {
			subDir := filepath.Join(p.tasksDir, parts[0])
			info, err := os.Stat(subDir)
			if err == nil && info.IsDir() {
				// Watch the new subdirectory
				if err := watcher.Add(subDir); err != nil {
					logging.Debug("failed to watch new task list directory", "dir", subDir, "error", err)
				}
				// Emit a sync event for the new list
				listID := parts[0]
				p.invalidateCache(listID)
				select {
				case events <- task.TaskEvent{Type: task.EventTypeListSync, ListID: listID}:
				case <-ctx.Done():
				}
			}
		}

	case 2:
		// Event inside a list subdirectory — a task file changed
		listID := parts[0]
		fileName := parts[1]

		// Only handle .json task files
		if filepath.Ext(fileName) != ".json" {
			return
		}

		p.invalidateCache(listID)

		select {
		case events <- task.TaskEvent{Type: task.EventTypeListSync, ListID: listID}:
		case <-ctx.Done():
		}
	}
}

// invalidateCache removes a list from the cache.
func (p *Provider) invalidateCache(listID string) {
	p.mu.Lock()
	delete(p.cache, listID)
	p.mu.Unlock()
}

// loadTaskDir loads all tasks from a list directory, using cache when available.
func (p *Provider) loadTaskDir(listID string) ([]taskRecord, error) {
	p.mu.RLock()
	if cached, ok := p.cache[listID]; ok {
		p.mu.RUnlock()
		return cached, nil
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	return p.loadTaskDirUnsafe(listID)
}

// loadTaskDirUnsafe loads all tasks from a list directory without locking.
func (p *Provider) loadTaskDirUnsafe(listID string) ([]taskRecord, error) {
	if cached, ok := p.cache[listID]; ok {
		return cached, nil
	}

	listDir := filepath.Join(p.tasksDir, listID)
	entries, err := os.ReadDir(listDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to read task list directory %s: %w", listDir, err)
	}

	var records []taskRecord
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		filePath := filepath.Join(listDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			logging.Debug("failed to read task file", "path", filePath, "error", err)
			continue
		}

		var tr taskRecord
		if err := json.Unmarshal(data, &tr); err != nil {
			logging.Debug("failed to parse task file", "path", filePath, "error", err)
			continue
		}

		records = append(records, tr)
	}

	// Sort by numeric ID for consistent ordering
	sort.Slice(records, func(i, j int) bool {
		ni, _ := strconv.Atoi(records[i].ID)
		nj, _ := strconv.Atoi(records[j].ID)
		return ni < nj
	})

	p.cache[listID] = records
	return records, nil
}

// nextTaskID reads the highwatermark or counts files to determine the next task ID.
func (p *Provider) nextTaskID(listDir string) int {
	hwPath := filepath.Join(listDir, ".highwatermark")
	data, err := os.ReadFile(hwPath)
	if err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && n > 0 {
			return n + 1
		}
	}

	// Fallback: find the highest numeric ID in existing files
	entries, err := os.ReadDir(listDir)
	if err != nil {
		return 1
	}

	maxID := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := entry.Name()[:len(entry.Name())-5]
		if n, err := strconv.Atoi(name); err == nil && n > maxID {
			maxID = n
		}
	}
	return maxID + 1
}

// writeTaskFile writes a single task record to its file.
func writeTaskFile(listDir string, tr taskRecord) error {
	filePath := filepath.Join(listDir, tr.ID+".json")
	data, err := json.MarshalIndent(tr, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write task file %s: %w", filePath, err)
	}
	return nil
}

// writeHighwatermark updates the highwatermark file.
func writeHighwatermark(listDir string, id int) {
	hwPath := filepath.Join(listDir, ".highwatermark")
	_ = os.WriteFile(hwPath, []byte(strconv.Itoa(id)), 0600)
}

// hasLockFile checks if a .lock file exists in the directory,
// indicating an agent is actively working on this task list.
func hasLockFile(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".lock"))
	return err == nil
}

// countTaskFiles counts .json task files in a directory.
func countTaskFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			count++
		}
	}
	return count
}

// recordToTask converts a storage record to a task.Task.
func recordToTask(listID string, tr taskRecord) task.Task {
	var createdAt time.Time
	if parsed, err := time.Parse(time.RFC3339, tr.CreatedAt); err == nil {
		createdAt = parsed
	}
	var updatedAt time.Time
	if parsed, err := time.Parse(time.RFC3339, tr.UpdatedAt); err == nil {
		updatedAt = parsed
	}

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
