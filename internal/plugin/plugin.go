// Package plugin provides the plugin system for external integrations.
// Plugins are categorized by type:
//   - vcs: Version Control Systems (GitHub, GitLab)
//   - pm: Project Management (Jira, Linear)
package plugin

import "sync"

// Category represents a plugin category.
type Category string

const (
	CategoryVCS Category = "vcs" // Version Control Systems
	CategoryPM  Category = "pm"  // Project Management
)

// Plugin is the base interface all plugins implement.
type Plugin interface {
	// Name returns the plugin identifier (e.g., "github", "linear")
	Name() string

	// Category returns the plugin category (vcs, pm)
	Category() Category

	// Enabled returns whether the plugin is enabled
	Enabled() bool

	// SetEnabled enables or disables the plugin
	SetEnabled(enabled bool)
}

// BasePlugin provides common plugin functionality.
type BasePlugin struct {
	name     string
	category Category
	mu       sync.RWMutex
	enabled  bool
}

// NewBasePlugin creates a base plugin.
func NewBasePlugin(name string, category Category) *BasePlugin {
	return &BasePlugin{
		name:     name,
		category: category,
		enabled:  false,
	}
}

func (p *BasePlugin) Name() string       { return p.name }
func (p *BasePlugin) Category() Category { return p.category }
func (p *BasePlugin) Enabled() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.enabled
}
func (p *BasePlugin) SetEnabled(e bool) {
	p.mu.Lock()
	p.enabled = e
	p.mu.Unlock()
}

// Registry manages all available plugins.
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]Plugin
}

// NewRegistry creates a new plugin registry.
func NewRegistry() *Registry {
	return &Registry{
		plugins: make(map[string]Plugin),
	}
}

// Register adds a plugin to the registry.
func (r *Registry) Register(p Plugin) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[p.Name()] = p
}

// Get returns a plugin by name.
func (r *Registry) Get(name string) Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.plugins[name]
}

// GetByCategory returns all plugins in a category.
func (r *Registry) GetByCategory(cat Category) []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Plugin
	for _, p := range r.plugins {
		if p.Category() == cat {
			result = append(result, p)
		}
	}
	return result
}

// GetEnabled returns all enabled plugins.
func (r *Registry) GetEnabled() []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Plugin
	for _, p := range r.plugins {
		if p.Enabled() {
			result = append(result, p)
		}
	}
	return result
}

// GetEnabledByCategory returns enabled plugins in a category.
func (r *Registry) GetEnabledByCategory(cat Category) []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Plugin
	for _, p := range r.plugins {
		if p.Category() == cat && p.Enabled() {
			result = append(result, p)
		}
	}
	return result
}

// List returns all registered plugins.
func (r *Registry) List() []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Plugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		result = append(result, p)
	}
	return result
}

// Enable enables a plugin by name.
func (r *Registry) Enable(name string) bool {
	r.mu.RLock()
	p, ok := r.plugins[name]
	r.mu.RUnlock()
	if ok {
		p.SetEnabled(true)
		return true
	}
	return false
}

// Disable disables a plugin by name.
func (r *Registry) Disable(name string) bool {
	r.mu.RLock()
	p, ok := r.plugins[name]
	r.mu.RUnlock()
	if ok {
		p.SetEnabled(false)
		return true
	}
	return false
}
