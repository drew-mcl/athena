package plugin

import (
	"testing"
)

func TestBasePlugin(t *testing.T) {
	p := NewBasePlugin("test-plugin", CategoryVCS)

	if p.Name() != "test-plugin" {
		t.Errorf("Name() = %q, want %q", p.Name(), "test-plugin")
	}
	if p.Category() != CategoryVCS {
		t.Errorf("Category() = %q, want %q", p.Category(), CategoryVCS)
	}
	if p.Enabled() {
		t.Error("new plugin should be disabled by default")
	}

	p.SetEnabled(true)
	if !p.Enabled() {
		t.Error("plugin should be enabled after SetEnabled(true)")
	}

	p.SetEnabled(false)
	if p.Enabled() {
		t.Error("plugin should be disabled after SetEnabled(false)")
	}
}

type mockPlugin struct {
	*BasePlugin
}

func newMockPlugin(name string, cat Category) *mockPlugin {
	return &mockPlugin{BasePlugin: NewBasePlugin(name, cat)}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()

	gh := newMockPlugin("github", CategoryVCS)
	gl := newMockPlugin("gitlab", CategoryVCS)
	jira := newMockPlugin("jira", CategoryPM)
	linear := newMockPlugin("linear", CategoryPM)

	r.Register(gh)
	r.Register(gl)
	r.Register(jira)
	r.Register(linear)

	t.Run("Get", func(t *testing.T) {
		if r.Get("github") != gh {
			t.Error("Get(github) returned wrong plugin")
		}
		if r.Get("nonexistent") != nil {
			t.Error("Get(nonexistent) should return nil")
		}
	})

	t.Run("List", func(t *testing.T) {
		all := r.List()
		if len(all) != 4 {
			t.Errorf("List() returned %d plugins, want 4", len(all))
		}
	})

	t.Run("GetByCategory", func(t *testing.T) {
		vcsPlugins := r.GetByCategory(CategoryVCS)
		if len(vcsPlugins) != 2 {
			t.Errorf("GetByCategory(vcs) returned %d, want 2", len(vcsPlugins))
		}

		pmPlugins := r.GetByCategory(CategoryPM)
		if len(pmPlugins) != 2 {
			t.Errorf("GetByCategory(pm) returned %d, want 2", len(pmPlugins))
		}
	})

	t.Run("GetEnabled empty", func(t *testing.T) {
		enabled := r.GetEnabled()
		if len(enabled) != 0 {
			t.Errorf("GetEnabled() returned %d, want 0", len(enabled))
		}
	})

	t.Run("Enable/Disable", func(t *testing.T) {
		if !r.Enable("github") {
			t.Error("Enable(github) should return true")
		}
		if r.Enable("nonexistent") {
			t.Error("Enable(nonexistent) should return false")
		}

		enabled := r.GetEnabled()
		if len(enabled) != 1 {
			t.Errorf("GetEnabled() after enable: got %d, want 1", len(enabled))
		}

		if !r.Disable("github") {
			t.Error("Disable(github) should return true")
		}
		if r.Disable("nonexistent") {
			t.Error("Disable(nonexistent) should return false")
		}

		enabled = r.GetEnabled()
		if len(enabled) != 0 {
			t.Errorf("GetEnabled() after disable: got %d, want 0", len(enabled))
		}
	})

	t.Run("GetEnabledByCategory", func(t *testing.T) {
		r.Enable("github")
		r.Enable("jira")

		vcsEnabled := r.GetEnabledByCategory(CategoryVCS)
		if len(vcsEnabled) != 1 {
			t.Errorf("GetEnabledByCategory(vcs) = %d, want 1", len(vcsEnabled))
		}

		pmEnabled := r.GetEnabledByCategory(CategoryPM)
		if len(pmEnabled) != 1 {
			t.Errorf("GetEnabledByCategory(pm) = %d, want 1", len(pmEnabled))
		}
	})
}

func TestCategories(t *testing.T) {
	if CategoryVCS != "vcs" {
		t.Errorf("CategoryVCS = %q, want %q", CategoryVCS, "vcs")
	}
	if CategoryPM != "pm" {
		t.Errorf("CategoryPM = %q, want %q", CategoryPM, "pm")
	}
}
