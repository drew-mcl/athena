package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndexer_IndexProject(t *testing.T) {
	// Get the project root (parent of internal/index)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	projectRoot := filepath.Join(wd, "..", "..")

	indexer, err := NewIndexer(projectRoot)
	if err != nil {
		t.Fatalf("NewIndexer failed: %v", err)
	}

	if indexer.ModulePath() != "github.com/drewfead/athena" {
		t.Errorf("unexpected module path: %s", indexer.ModulePath())
	}

	idx, err := indexer.IndexProject()
	if err != nil {
		t.Fatalf("IndexProject failed: %v", err)
	}

	// Verify we got some symbols
	stats := idx.Stats()
	if stats.TotalSymbols == 0 {
		t.Error("expected some symbols to be indexed")
	}
	if stats.TotalFiles == 0 {
		t.Error("expected some files to be indexed")
	}
	if stats.TotalDeps == 0 {
		t.Error("expected some dependencies to be indexed")
	}

	t.Logf("Indexed %d symbols in %d files with %d dependencies", stats.TotalSymbols, stats.TotalFiles, stats.TotalDeps)

	// Test symbol lookup - should find the Index type
	indexSymbols := idx.LookupSymbol("Index")
	if len(indexSymbols) == 0 {
		t.Error("expected to find Index symbol")
	}

	// Test symbol lookup - should find the NewIndexer function
	newIndexerSymbols := idx.LookupSymbol("NewIndexer")
	if len(newIndexerSymbols) == 0 {
		t.Error("expected to find NewIndexer symbol")
	}

	// Verify NewIndexer is a func
	found := false
	for _, sym := range newIndexerSymbols {
		if sym.Kind == SymbolKindFunc {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected NewIndexer to be a func")
	}
}

func TestIndexer_IndexFile(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	projectRoot := filepath.Join(wd, "..", "..")

	indexer, err := NewIndexer(projectRoot)
	if err != nil {
		t.Fatalf("NewIndexer failed: %v", err)
	}

	// Index this test file
	symbols, deps, err := indexer.IndexFile(filepath.Join(wd, "index_test.go"))
	if err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}

	// Should find test functions
	found := false
	for _, sym := range symbols {
		if sym.Name == "TestIndexer_IndexProject" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find TestIndexer_IndexProject function")
	}

	// Should have testing import
	foundTesting := false
	for _, dep := range deps {
		if dep.ImportPath == "testing" {
			foundTesting = true
			break
		}
	}
	if !foundTesting {
		t.Error("expected to find testing import")
	}
}

func TestExtractSymbols(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	symbols, err := ExtractSymbols(filepath.Join(wd, "types.go"), "internal/index/types.go")
	if err != nil {
		t.Fatalf("ExtractSymbols failed: %v", err)
	}

	// Check we found some expected symbols
	symbolMap := make(map[string]Symbol)
	for _, sym := range symbols {
		symbolMap[sym.Name] = sym
	}

	// Check Index struct
	if sym, ok := symbolMap["Index"]; !ok {
		t.Error("expected to find Index type")
	} else if sym.Kind != SymbolKindStruct {
		t.Errorf("expected Index to be struct, got %s", sym.Kind)
	}

	// Check NewIndex function
	if sym, ok := symbolMap["NewIndex"]; !ok {
		t.Error("expected to find NewIndex function")
	} else if sym.Kind != SymbolKindFunc {
		t.Errorf("expected NewIndex to be func, got %s", sym.Kind)
	}

	// Check SymbolKind type
	if sym, ok := symbolMap["SymbolKind"]; !ok {
		t.Error("expected to find SymbolKind type")
	} else if sym.Kind != SymbolKindType {
		t.Errorf("expected SymbolKind to be type, got %s", sym.Kind)
	}

	// Check constants
	if _, ok := symbolMap["SymbolKindFunc"]; !ok {
		t.Error("expected to find SymbolKindFunc constant")
	}
}

func TestExtractDependencies(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	deps, err := ExtractDependencies(
		filepath.Join(wd, "index.go"),
		"internal/index/index.go",
		"github.com/drewfead/athena",
	)
	if err != nil {
		t.Fatalf("ExtractDependencies failed: %v", err)
	}

	// Should have some standard library imports
	foundCrypto := false
	foundFilepath := false
	for _, dep := range deps {
		if dep.ImportPath == "crypto/sha256" {
			foundCrypto = true
		}
		if dep.ImportPath == "path/filepath" {
			foundFilepath = true
		}
	}

	if !foundCrypto {
		t.Error("expected to find crypto/sha256 import")
	}
	if !foundFilepath {
		t.Error("expected to find path/filepath import")
	}
}

func TestSearchSymbols(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	projectRoot := filepath.Join(wd, "..", "..")

	indexer, err := NewIndexer(projectRoot)
	if err != nil {
		t.Fatalf("NewIndexer failed: %v", err)
	}

	idx, err := indexer.IndexProject()
	if err != nil {
		t.Fatalf("IndexProject failed: %v", err)
	}

	// Exact match
	results := SearchSymbols(idx, "Index", true)
	if len(results) == 0 {
		t.Error("expected exact match for Index")
	}

	// Prefix match
	results = SearchSymbols(idx, "Symbol", false)
	if len(results) == 0 {
		t.Error("expected prefix match for Symbol")
	}

	// Should find SymbolKind, SymbolKindFunc, etc.
	foundKind := false
	for _, sym := range results {
		if sym.Name == "SymbolKind" {
			foundKind = true
			break
		}
	}
	if !foundKind {
		t.Error("expected to find SymbolKind in prefix search for Symbol")
	}
}

func TestDependencyGraph(t *testing.T) {
	deps := []Dependency{
		{FromFile: "a.go", ToFile: "pkg/b", IsInternal: true, ImportPath: "test/pkg/b"},
		{FromFile: "a.go", ToFile: "pkg/c", IsInternal: true, ImportPath: "test/pkg/c"},
		{FromFile: "pkg/b/b.go", ToFile: "pkg/c", IsInternal: true, ImportPath: "test/pkg/c"},
	}

	graph := BuildDependencyGraph(deps)

	// a.go's directory "." should depend on pkg/b and pkg/c
	if len(graph["."]) != 2 {
		t.Errorf("expected . to have 2 dependencies, got %d", len(graph["."]))
	}

	// pkg/b should depend on pkg/c
	if len(graph["pkg/b"]) != 1 {
		t.Errorf("expected pkg/b to have 1 dependency, got %d", len(graph["pkg/b"]))
	}
}
