package index

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Indexer coordinates the indexing of a Go project.
type Indexer struct {
	projectPath string
	modulePath  string
}

// NewIndexer creates a new Indexer for the given project path.
func NewIndexer(projectPath string) (*Indexer, error) {
	// Resolve to absolute path
	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, fmt.Errorf("resolve project path: %w", err)
	}

	// Verify the path exists
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("stat project path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("project path is not a directory: %s", absPath)
	}

	// Resolve module path from go.mod
	modulePath, err := ResolveModulePath(absPath)
	if err != nil {
		return nil, fmt.Errorf("resolve module path: %w", err)
	}
	if modulePath == "" {
		return nil, fmt.Errorf("no module path found in go.mod")
	}

	return &Indexer{
		projectPath: absPath,
		modulePath:  modulePath,
	}, nil
}

// IndexProject walks the project directory and indexes all Go files.
// Returns a complete Index with symbols and dependencies.
func (i *Indexer) IndexProject() (*Index, error) {
	// Generate a hash for this version of the project
	projectHash, err := i.computeProjectHash()
	if err != nil {
		return nil, fmt.Errorf("compute project hash: %w", err)
	}

	idx := NewIndex(i.projectPath, projectHash, i.modulePath)

	// Find all Go files
	files, err := WalkGoFiles(i.projectPath)
	if err != nil {
		return nil, fmt.Errorf("walk go files: %w", err)
	}

	// Process each file
	for _, filePath := range files {
		relativePath, err := filepath.Rel(i.projectPath, filePath)
		if err != nil {
			return nil, fmt.Errorf("compute relative path for %s: %w", filePath, err)
		}

		// Extract symbols
		symbols, err := ExtractSymbols(filePath, relativePath)
		if err != nil {
			// Log but continue - some files may have parse errors
			continue
		}
		for _, sym := range symbols {
			idx.AddSymbol(sym)
		}

		// Extract dependencies
		deps, err := ExtractDependencies(filePath, relativePath, i.modulePath)
		if err != nil {
			// Log but continue
			continue
		}
		for _, dep := range deps {
			idx.AddDependency(dep)
		}
	}

	return idx, nil
}

// IndexFile indexes a single file and returns its symbols and dependencies.
func (i *Indexer) IndexFile(filePath string) ([]Symbol, []Dependency, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve file path: %w", err)
	}

	relativePath, err := filepath.Rel(i.projectPath, absPath)
	if err != nil {
		return nil, nil, fmt.Errorf("compute relative path: %w", err)
	}

	symbols, err := ExtractSymbols(absPath, relativePath)
	if err != nil {
		return nil, nil, fmt.Errorf("extract symbols: %w", err)
	}

	deps, err := ExtractDependencies(absPath, relativePath, i.modulePath)
	if err != nil {
		return nil, nil, fmt.Errorf("extract dependencies: %w", err)
	}

	return symbols, deps, nil
}

// IndexDirectory indexes all Go files in a single directory (non-recursive).
func (i *Indexer) IndexDirectory(dirPath string) ([]Symbol, []Dependency, error) {
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve directory path: %w", err)
	}

	relativePath, err := filepath.Rel(i.projectPath, absPath)
	if err != nil {
		return nil, nil, fmt.Errorf("compute relative path: %w", err)
	}

	symbols, err := ExtractSymbolsFromDir(absPath, relativePath)
	if err != nil {
		return nil, nil, fmt.Errorf("extract symbols: %w", err)
	}

	deps, err := ExtractDependenciesFromDir(absPath, relativePath, i.modulePath)
	if err != nil {
		return nil, nil, fmt.Errorf("extract dependencies: %w", err)
	}

	return symbols, deps, nil
}

// computeProjectHash generates a hash identifying the current state of the project.
// Uses file modification times to detect changes quickly.
func (i *Indexer) computeProjectHash() (string, error) {
	files, err := WalkGoFiles(i.projectPath)
	if err != nil {
		return "", err
	}

	// Sort for deterministic hashing
	sort.Strings(files)

	h := sha256.New()

	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}

		// Include file path and modification time
		relPath, _ := filepath.Rel(i.projectPath, file)
		h.Write([]byte(relPath))
		h.Write([]byte(info.ModTime().Format(time.RFC3339Nano)))
	}

	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// ProjectPath returns the absolute project path.
func (i *Indexer) ProjectPath() string {
	return i.projectPath
}

// ComputeHash generates a hash identifying the current state of the project.
// This can be used to check if a cached index is still valid.
func (i *Indexer) ComputeHash() (string, error) {
	return i.computeProjectHash()
}

// ModulePath returns the Go module path.
func (i *Indexer) ModulePath() string {
	return i.modulePath
}

// FindRelatedFiles finds files related to a given file through dependencies.
// depth controls how many levels of dependencies to traverse.
func FindRelatedFiles(idx *Index, filePath string, depth int) []string {
	if depth <= 0 || idx == nil {
		return nil
	}

	fileDir := filepath.Dir(filePath)
	visited := map[string]bool{fileDir: true}

	var related []string

	queue := []queueItem{{dir: fileDir, depth: 0}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.depth >= depth {
			continue
		}

		enqueueDependencies(idx, current, visited, &related, &queue)
		enqueueDependents(idx, current, visited, &related, &queue)
	}

	return related
}

type queueItem struct {
	dir   string
	depth int
}

func enqueueDependencies(idx *Index, current queueItem, visited map[string]bool, related *[]string, queue *[]queueItem) {
	for file, deps := range idx.Deps {
		if filepath.Dir(file) != current.dir {
			continue
		}
		for _, dep := range deps {
			if !dep.IsInternal || dep.ToFile == "" {
				continue
			}
			targetDir := dep.ToFile
			if visited[targetDir] {
				continue
			}
			visited[targetDir] = true
			appendFilesFromDir(idx.FileSymbols, targetDir, related)
			*queue = append(*queue, queueItem{dir: targetDir, depth: current.depth + 1})
		}
	}
}

func enqueueDependents(idx *Index, current queueItem, visited map[string]bool, related *[]string, queue *[]queueItem) {
	for targetDir, dependentFiles := range idx.Dependents {
		if targetDir != current.dir {
			continue
		}
		for _, dependent := range dependentFiles {
			depDir := filepath.Dir(dependent)
			if visited[depDir] {
				continue
			}
			visited[depDir] = true
			appendFilesFromDir(idx.FileSymbols, depDir, related)
			*queue = append(*queue, queueItem{dir: depDir, depth: current.depth + 1})
		}
	}
}

func appendFilesFromDir(fileSymbols map[string][]Symbol, dir string, related *[]string) {
	for targetFile := range fileSymbols {
		if filepath.Dir(targetFile) == dir {
			*related = append(*related, targetFile)
		}
	}
}

// SearchSymbols searches for symbols matching a pattern.
// Currently supports exact match and prefix match.
func SearchSymbols(idx *Index, pattern string, exactMatch bool) []Symbol {
	if idx == nil {
		return nil
	}

	var results []Symbol

	if exactMatch {
		return idx.LookupSymbol(pattern)
	}

	// Prefix match
	for name, symbols := range idx.Symbols {
		if len(name) >= len(pattern) && name[:len(pattern)] == pattern {
			results = append(results, symbols...)
		}
	}

	return results
}

// GetPackageSymbols returns all symbols in a package (directory).
func GetPackageSymbols(idx *Index, pkgDir string) []Symbol {
	if idx == nil {
		return nil
	}

	var results []Symbol

	for file, symbols := range idx.FileSymbols {
		if filepath.Dir(file) == pkgDir {
			results = append(results, symbols...)
		}
	}

	return results
}

// SymbolCount returns the total number of symbols in the index.
func (idx *Index) SymbolCount() int {
	return idx.Stats().TotalSymbols
}

// FileCount returns the number of files in the index.
func (idx *Index) FileCount() int {
	return idx.Stats().TotalFiles
}

// DependencyCount returns the total number of dependency edges.
func (idx *Index) DependencyCount() int {
	return idx.Stats().TotalDeps
}

// GetFileDependencies returns file paths that the given file depends on (internal only).
func (idx *Index) GetFileDependencies(filePath string) []string {
	if idx == nil {
		return nil
	}
	deps := idx.Deps[filePath]
	var files []string
	for _, dep := range deps {
		if dep.IsInternal && dep.ToFile != "" {
			files = append(files, dep.ToFile)
		}
	}
	return files
}
