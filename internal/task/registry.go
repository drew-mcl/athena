package task

import (
	"context"
	"fmt"
	"sync"

	"github.com/drewfead/athena/internal/logging"
)

// Registry manages multiple task providers and aggregates their results.
type Registry struct {
	providers map[string]Provider
	mu        sync.RWMutex
}

// NewRegistry creates a new task provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

// Register adds a provider to the registry.
func (r *Registry) Register(provider Provider) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := provider.Name()
	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("provider %q already registered", name)
	}
	r.providers[name] = provider
	return nil
}

// Unregister removes a provider from the registry.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.providers, name)
}

// Provider returns a specific provider by name.
func (r *Registry) Provider(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

// Providers returns all registered providers.
func (r *Registry) Providers() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		result = append(result, p)
	}
	return result
}

// ProviderNames returns the names of all registered providers.
func (r *Registry) ProviderNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// ListAllTaskLists returns task lists from all providers.
func (r *Registry) ListAllTaskLists() ([]TaskList, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var allLists []TaskList
	for _, p := range r.providers {
		lists, err := p.ListTaskLists()
		if err != nil {
			logging.Warn("failed to list tasks from provider", "provider", p.Name(), "error", err)
			continue
		}
		allLists = append(allLists, lists...)
	}
	return allLists, nil
}

// ListTasks returns tasks from a specific list.
// The listID should be prefixed with the provider name (e.g., "claude:list-123").
func (r *Registry) ListTasks(providerName, listID string, filters TaskFilters) ([]Task, error) {
	r.mu.RLock()
	p, ok := r.providers[providerName]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("provider %q not found", providerName)
	}
	return p.ListTasks(listID, filters)
}

// GetTask retrieves a specific task.
func (r *Registry) GetTask(providerName, listID, taskID string) (*Task, error) {
	r.mu.RLock()
	p, ok := r.providers[providerName]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("provider %q not found", providerName)
	}
	return p.GetTask(listID, taskID)
}

// CreateTask creates a new task in the specified list.
func (r *Registry) CreateTask(providerName, listID string, task *TaskCreate) (*Task, error) {
	r.mu.RLock()
	p, ok := r.providers[providerName]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("provider %q not found", providerName)
	}
	return p.CreateTask(listID, task)
}

// UpdateTask modifies an existing task.
func (r *Registry) UpdateTask(providerName, listID, taskID string, update *TaskUpdate) (*Task, error) {
	r.mu.RLock()
	p, ok := r.providers[providerName]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("provider %q not found", providerName)
	}
	return p.UpdateTask(listID, taskID, update)
}

// DeleteTask removes a task.
func (r *Registry) DeleteTask(providerName, listID, taskID string) error {
	r.mu.RLock()
	p, ok := r.providers[providerName]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("provider %q not found", providerName)
	}
	return p.DeleteTask(listID, taskID)
}

// WatchAll starts watching all providers and merges events into a single channel.
func (r *Registry) WatchAll(ctx context.Context) (<-chan TaskEvent, error) {
	r.mu.RLock()
	providers := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		providers = append(providers, p)
	}
	r.mu.RUnlock()

	merged := make(chan TaskEvent, 100)

	var wg sync.WaitGroup
	for _, p := range providers {
		ch, err := p.Watch(ctx)
		if err != nil {
			logging.Debug("provider watch failed", "provider", p.Name(), "error", err)
			continue
		}

		wg.Add(1)
		go func(ch <-chan TaskEvent) {
			defer wg.Done()
			for event := range ch {
				select {
				case merged <- event:
				case <-ctx.Done():
					return
				}
			}
		}(ch)
	}

	// Close merged channel when all watchers are done
	go func() {
		wg.Wait()
		close(merged)
	}()

	return merged, nil
}
