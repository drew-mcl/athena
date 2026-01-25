package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple task",
			input:    "fix the auth bug",
			expected: []string{"auth", "bug"},
		},
		{
			name:     "camelCase identifier",
			input:    "implement SpawnAgent method",
			expected: []string{"SpawnAgent"},
		},
		{
			name:     "snake_case identifier",
			input:    "update user_profile handler",
			expected: []string{"user_profile", "handler"},
		},
		{
			name:     "mixed content",
			input:    "Add error handling to SpawnAgent in the daemon package",
			expected: []string{"SpawnAgent", "error", "handling", "daemon", "package"},
		},
		{
			name:     "stop words removed",
			input:    "the is a an to from in on at for of and or",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractKeywords(tt.input)

			// Check that all expected keywords are present
			resultSet := make(map[string]bool)
			for _, kw := range result {
				resultSet[kw] = true
			}

			for _, exp := range tt.expected {
				if !resultSet[exp] {
					t.Errorf("expected keyword %q not found in result %v", exp, result)
				}
			}
		})
	}
}

func TestSplitIdentifier(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"SpawnAgent", []string{"Spawn", "Agent"}},
		{"user_profile", []string{"user", "profile"}},
		{"handleHTTPRequest", []string{"handle", "H", "T", "T", "P", "Request"}},
		{"simple", []string{"simple"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := splitIdentifier(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("expected %v, got %v", tt.expected, result)
					break
				}
			}
		})
	}
}

func TestComputePathScore(t *testing.T) {
	scorer := NewScorer(nil)

	tests := []struct {
		path     string
		keywords []string
		minScore float64
	}{
		{"internal/agent/spawner.go", []string{"agent", "spawn"}, 0.5},
		{"internal/daemon/daemon.go", []string{"daemon"}, 0.5},
		{"internal/store/sqlite.go", []string{"agent"}, 0.0},
		{"internal/auth/handler.go", []string{"auth", "handle"}, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			score := scorer.computePathScore(tt.path, tt.keywords)
			if score < tt.minScore {
				t.Errorf("path %q with keywords %v: expected score >= %v, got %v",
					tt.path, tt.keywords, tt.minScore, score)
			}
		})
	}
}

func TestComputeSizePenalty(t *testing.T) {
	scorer := NewScorer(nil)

	tests := []struct {
		size       int
		maxPenalty float64
	}{
		{0, 0},
		{1024, 0},           // 1KB - no penalty
		{5 * 1024, 0.01},    // 5KB - minimal penalty
		{10 * 1024, 0.20},   // 10KB - small penalty
		{50 * 1024, 0.55},   // 50KB - medium penalty
		{100 * 1024, 0.75},  // 100KB - large penalty
		{500 * 1024, 1.0},   // 500KB - max penalty
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			penalty := scorer.computeSizePenalty(tt.size)
			if penalty > tt.maxPenalty {
				t.Errorf("size %d: expected penalty <= %v, got %v",
					tt.size, tt.maxPenalty, penalty)
			}
		})
	}
}

func TestScoreFilesWithIndex(t *testing.T) {
	// Create a mock index
	idx := NewIndex("/project", "test-hash", "github.com/test/project")

	// Add some symbols
	idx.AddSymbol(Symbol{
		Name:     "SpawnAgent",
		Kind:     SymbolKindFunc,
		FilePath: "/project/internal/agent/spawner.go",
	})
	idx.AddSymbol(Symbol{
		Name:     "Daemon",
		Kind:     SymbolKindStruct,
		FilePath: "/project/internal/daemon/daemon.go",
	})
	idx.AddSymbol(Symbol{
		Name:     "Store",
		Kind:     SymbolKindStruct,
		FilePath: "/project/internal/store/sqlite.go",
	})

	// Add some dependencies
	idx.AddDependency(Dependency{
		FromFile:   "/project/internal/daemon/daemon.go",
		ToFile:     "/project/internal/agent/spawner.go",
		DepType:    DepTypeImport,
		IsInternal: true,
	})
	idx.AddDependency(Dependency{
		FromFile:   "/project/internal/daemon/daemon.go",
		ToFile:     "/project/internal/store/sqlite.go",
		DepType:    DepTypeImport,
		IsInternal: true,
	})

	scorer := NewScorer(idx)

	// Test symbol lookup
	symbols := idx.LookupSymbol("SpawnAgent")
	if len(symbols) != 1 {
		t.Errorf("expected 1 symbol for SpawnAgent, got %d", len(symbols))
	}

	// Test dependency distance
	dist := idx.GetDependencyDistance("/project/internal/daemon/daemon.go", "/project/internal/agent/spawner.go")
	if dist != 1 {
		t.Errorf("expected distance 1, got %d", dist)
	}

	// Verify scorer was created
	if scorer.index == nil {
		t.Error("scorer index should not be nil")
	}
}

func TestScoreFilesIntegration(t *testing.T) {
	// Create a temp directory with some Go files
	tmpDir, err := os.MkdirTemp("", "relevance_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	testFiles := map[string]string{
		"internal/agent/spawner.go": `package agent
func SpawnAgent() {}
`,
		"internal/daemon/daemon.go": `package daemon
type Daemon struct {}
`,
		"internal/store/sqlite.go": `package store
type Store struct {}
`,
	}

	for relPath, content := range testFiles {
		fullPath := filepath.Join(tmpDir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}
	}

	// Score files without index
	scorer := NewScorer(nil)
	results := scorer.ScoreFiles("fix SpawnAgent in the agent package", tmpDir, 10)

	// Verify we got results
	if len(results) == 0 {
		t.Error("expected some scored files, got none")
	}

	// The agent/spawner.go file should score highest
	if len(results) > 0 {
		if !containsSubstr(results[0].Path, "agent") {
			t.Errorf("expected agent file to score highest, got %s", results[0].Path)
		}
	}
}

func TestDependencyDistance(t *testing.T) {
	idx := NewIndex("/project", "test-hash", "github.com/test/project")

	// Create a dependency chain: A -> B -> C
	idx.AddDependency(Dependency{
		FromFile:   "/a.go",
		ToFile:     "/b.go",
		DepType:    DepTypeImport,
		IsInternal: true,
	})
	idx.AddDependency(Dependency{
		FromFile:   "/b.go",
		ToFile:     "/c.go",
		DepType:    DepTypeImport,
		IsInternal: true,
	})

	tests := []struct {
		from     string
		to       string
		expected int
	}{
		{"/a.go", "/a.go", 0},
		{"/a.go", "/b.go", 1},
		{"/a.go", "/c.go", 2},
		{"/c.go", "/a.go", 2}, // Reverse direction also works
		{"/a.go", "/d.go", -1}, // No path
	}

	for _, tt := range tests {
		t.Run(tt.from+"->"+tt.to, func(t *testing.T) {
			dist := idx.GetDependencyDistance(tt.from, tt.to)
			if dist != tt.expected {
				t.Errorf("expected distance %d, got %d", tt.expected, dist)
			}
		})
	}
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
