// Package index provides code indexing for Go projects using AST parsing.
// It extracts symbols (functions, structs, interfaces, etc.) and builds
// dependency graphs to enable fast code lookup and navigation.
package index

// SymbolKind represents the type of a symbol.
type SymbolKind string

const (
	SymbolKindFunc      SymbolKind = "func"
	SymbolKindStruct    SymbolKind = "struct"
	SymbolKindInterface SymbolKind = "interface"
	SymbolKindConst     SymbolKind = "const"
	SymbolKindVar       SymbolKind = "var"
	SymbolKindMethod    SymbolKind = "method"
	SymbolKindType      SymbolKind = "type" // type alias or named type
)

// Symbol represents a Go symbol extracted from source code.
type Symbol struct {
	Name       string     // Symbol name (e.g., "Daemon", "NewDaemon")
	Kind       SymbolKind // func, struct, interface, etc.
	FilePath   string     // Relative path from project root
	LineNumber int        // Line number where symbol is defined
	Package    string     // Package name (e.g., "daemon")
	Receiver   string     // For methods, the receiver type (e.g., "*Daemon")
	Exported   bool       // Whether the symbol is exported (starts with uppercase)
}

// DepType represents the type of dependency between files.
type DepType string

const (
	DepTypeImport DepType = "import" // File imports another package
)

// Dependency represents a dependency relationship between files.
type Dependency struct {
	FromFile   string  // File that has the dependency
	ToFile     string  // File being depended on (or package path for external)
	DepType    DepType // Type of dependency
	ImportPath string  // Full import path (e.g., "github.com/drewfead/athena/internal/store")
	IsInternal bool    // Whether this is an internal project import
}

// Index holds the complete code index for a project.
type Index struct {
	ProjectPath string                  // Root path of the project
	ProjectHash string                  // Hash identifying this version of the index
	ModulePath  string                  // Go module path (e.g., "github.com/drewfead/athena")
	Symbols     map[string][]Symbol     // Map of symbol name to locations (multiple files can define same name)
	FileSymbols map[string][]Symbol     // Map of file path to symbols defined in that file
	Deps        map[string][]Dependency // Map of file path to its dependencies
	Dependents  map[string][]string     // Map of file path to files that depend on it
}

// NewIndex creates an empty Index for a project.
func NewIndex(projectPath, projectHash, modulePath string) *Index {
	return &Index{
		ProjectPath: projectPath,
		ProjectHash: projectHash,
		ModulePath:  modulePath,
		Symbols:     make(map[string][]Symbol),
		FileSymbols: make(map[string][]Symbol),
		Deps:        make(map[string][]Dependency),
		Dependents:  make(map[string][]string),
	}
}

// AddSymbol adds a symbol to the index.
func (idx *Index) AddSymbol(sym Symbol) {
	if idx == nil {
		return
	}
	idx.Symbols[sym.Name] = append(idx.Symbols[sym.Name], sym)
	idx.FileSymbols[sym.FilePath] = append(idx.FileSymbols[sym.FilePath], sym)
}

// AddDependency adds a dependency to the index.
func (idx *Index) AddDependency(dep Dependency) {
	if idx == nil {
		return
	}
	idx.Deps[dep.FromFile] = append(idx.Deps[dep.FromFile], dep)
	if dep.IsInternal && dep.ToFile != "" {
		idx.Dependents[dep.ToFile] = append(idx.Dependents[dep.ToFile], dep.FromFile)
	}
}

// LookupSymbol finds all locations where a symbol is defined.
func (idx *Index) LookupSymbol(name string) []Symbol {
	if idx == nil {
		return nil
	}
	return idx.Symbols[name]
}

// GetFileSymbols returns all symbols defined in a file.
func (idx *Index) GetFileSymbols(filePath string) []Symbol {
	if idx == nil {
		return nil
	}
	return idx.FileSymbols[filePath]
}

// GetDependencies returns all dependencies for a file.
func (idx *Index) GetDependencies(filePath string) []Dependency {
	if idx == nil {
		return nil
	}
	return idx.Deps[filePath]
}

// GetDependents returns all files that depend on the given file.
func (idx *Index) GetDependents(filePath string) []string {
	if idx == nil {
		return nil
	}
	return idx.Dependents[filePath]
}

// GetDependencyDistance computes the shortest path between two files in the dependency graph.
// Returns -1 if no path exists.
func (idx *Index) GetDependencyDistance(fromFile, toFile string) int {
	if idx == nil {
		return -1
	}

	if fromFile == toFile {
		return 0
	}

	visited := map[string]bool{fromFile: true}
	queue := []dependencyQueueItem{{file: fromFile, depth: 0}}
	return idx.bfsDependencyDistance(queue, visited, toFile)
}

type dependencyQueueItem struct {
	file  string
	depth int
}

func (idx *Index) bfsDependencyDistance(queue []dependencyQueueItem, visited map[string]bool, target string) int {
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.depth > 10 {
			break
		}

		for _, neighbor := range idx.dependencyNeighbors(current.file) {
			if neighbor == target {
				return current.depth + 1
			}
			if visited[neighbor] {
				continue
			}
			visited[neighbor] = true
			queue = append(queue, dependencyQueueItem{file: neighbor, depth: current.depth + 1})
		}
	}
	return -1
}

func (idx *Index) dependencyNeighbors(file string) []string {
	var neighbors []string
	for _, dep := range idx.Deps[file] {
		if dep.IsInternal && dep.ToFile != "" {
			neighbors = append(neighbors, dep.ToFile)
		}
	}
	return append(neighbors, idx.Dependents[file]...)
}

// Stats returns statistics about the index.
func (idx *Index) Stats() IndexStats {
	if idx == nil {
		return IndexStats{}
	}
	totalSymbols := 0
	for _, syms := range idx.FileSymbols {
		totalSymbols += len(syms)
	}
	totalDeps := 0
	for _, deps := range idx.Deps {
		totalDeps += len(deps)
	}
	return IndexStats{
		TotalSymbols: totalSymbols,
		TotalFiles:   len(idx.FileSymbols),
		TotalDeps:    totalDeps,
		UniqueNames:  len(idx.Symbols),
	}
}

// IndexStats holds statistics about an index.
type IndexStats struct {
	TotalSymbols int // Total number of symbol definitions
	TotalFiles   int // Number of files indexed
	TotalDeps    int // Total number of dependencies
	UniqueNames  int // Number of unique symbol names
}
