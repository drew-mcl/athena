package index

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

const testFileSuffix = "_test.go"

// ExtractDependencies parses a Go file and extracts all import dependencies.
// filePath should be the absolute path to the file.
// relativePath is the path relative to the project root (used for storage).
// modulePath is the Go module path (e.g., "github.com/drewfead/athena").
func ExtractDependencies(filePath, relativePath, modulePath string) ([]Dependency, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}

	var deps []Dependency

	for _, imp := range file.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		deps = append(deps, buildDependency(relativePath, modulePath, importPath))
	}

	return deps, nil
}

// ExtractDependenciesFromDir parses all Go files in a directory and extracts dependencies.
// It does not recurse into subdirectories.
func ExtractDependenciesFromDir(dirPath, relativeDir, modulePath string) ([]Dependency, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dirPath, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}

	var deps []Dependency

	for _, pkg := range pkgs {
		// Skip test packages
		if strings.HasSuffix(pkg.Name, "_test") {
			continue
		}
		deps = appendPackageDependencies(deps, pkg, relativeDir, modulePath)
	}

	return deps, nil
}

func appendPackageDependencies(deps []Dependency, pkg *ast.Package, relativeDir, modulePath string) []Dependency {
	for fileName, file := range pkg.Files {
		if strings.HasSuffix(fileName, testFileSuffix) {
			continue
		}
		relativePath := filepath.Join(relativeDir, filepath.Base(fileName))
		deps = appendFileDependencies(deps, file, relativePath, modulePath)
	}
	return deps
}

func appendFileDependencies(deps []Dependency, file *ast.File, relativePath, modulePath string) []Dependency {
	for _, imp := range file.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		deps = append(deps, buildDependency(relativePath, modulePath, importPath))
	}
	return deps
}

func buildDependency(relativePath, modulePath, importPath string) Dependency {
	dep := Dependency{
		FromFile:   relativePath,
		DepType:    DepTypeImport,
		ImportPath: importPath,
		IsInternal: strings.HasPrefix(importPath, modulePath),
	}

	if dep.IsInternal {
		dep.ToFile = internalImportTarget(importPath, modulePath)
	}

	return dep
}

func internalImportTarget(importPath, modulePath string) string {
	relImportPath := strings.TrimPrefix(importPath, modulePath)
	return strings.TrimPrefix(relImportPath, "/")
}

// ResolveModulePath reads the go.mod file to determine the module path.
func ResolveModulePath(projectPath string) (string, error) {
	goModPath := filepath.Join(projectPath, "go.mod")
	content, err := os.ReadFile(goModPath)
	if err != nil {
		return "", err
	}

	// Simple parsing: find the "module" line
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			modulePath := strings.TrimPrefix(line, "module ")
			modulePath = strings.TrimSpace(modulePath)
			return modulePath, nil
		}
	}

	return "", nil
}

// BuildDependencyGraph builds a complete dependency graph for internal packages.
// It returns a map of source package to list of dependent packages.
func BuildDependencyGraph(deps []Dependency) map[string][]string {
	graph := make(map[string][]string)

	for _, dep := range deps {
		sourceDir, targetDir, ok := dependencyDirs(dep)
		if !ok {
			continue
		}
		graph[sourceDir] = appendUnique(graph[sourceDir], targetDir)
	}

	return graph
}

func dependencyDirs(dep Dependency) (string, string, bool) {
	if !dep.IsInternal || dep.ToFile == "" {
		return "", "", false
	}
	sourceDir := filepath.Dir(dep.FromFile)
	targetDir := dep.ToFile
	if sourceDir == targetDir {
		return "", "", false
	}
	return sourceDir, targetDir, true
}

func appendUnique(values []string, target string) []string {
	for _, value := range values {
		if value == target {
			return values
		}
	}
	return append(values, target)
}

// FindPackageFiles returns all non-test Go files in a directory.
func FindPackageFiles(dirPath string) ([]string, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, testFileSuffix) {
			files = append(files, filepath.Join(dirPath, name))
		}
	}

	return files, nil
}

// WalkGoFiles walks a directory tree and returns all Go file paths (non-test).
func WalkGoFiles(root string) ([]string, error) {
	var files []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip hidden directories and common non-source directories
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}

		// Only include non-test Go files
		name := info.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, testFileSuffix) {
			files = append(files, path)
		}

		return nil
	})

	return files, err
}

// WalkGoPackages walks a directory tree and returns all directory paths containing Go files.
func WalkGoPackages(root string) ([]string, error) {
	pkgDirs := make(map[string]bool)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip hidden directories and common non-source directories
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}

		// Only consider non-test Go files
		name := info.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, testFileSuffix) {
			pkgDirs[filepath.Dir(path)] = true
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Convert map to slice
	var dirs []string
	for dir := range pkgDirs {
		dirs = append(dirs, dir)
	}

	return dirs, nil
}

// TopologicalSort performs a topological sort on the dependency graph.
// Returns packages in order such that dependencies come before dependents.
func TopologicalSort(graph map[string][]string) ([]string, error) {
	inDegree, allNodes := buildInDegree(graph)
	ensureInDegreeEntries(inDegree, allNodes)
	queue := initialQueue(inDegree)
	return processQueue(queue, graph, inDegree), nil
}

func buildInDegree(graph map[string][]string) (map[string]int, map[string]bool) {
	inDegree := make(map[string]int)
	allNodes := make(map[string]bool)
	for node, deps := range graph {
		allNodes[node] = true
		if _, ok := inDegree[node]; !ok {
			inDegree[node] = 0
		}
		for _, dep := range deps {
			allNodes[dep] = true
			inDegree[dep]++
		}
	}
	return inDegree, allNodes
}

func ensureInDegreeEntries(inDegree map[string]int, allNodes map[string]bool) {
	for node := range allNodes {
		if _, ok := inDegree[node]; !ok {
			inDegree[node] = 0
		}
	}
}

func initialQueue(inDegree map[string]int) []string {
	var queue []string
	for node, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, node)
		}
	}
	return queue
}

func processQueue(queue []string, graph map[string][]string, inDegree map[string]int) []string {
	var sorted []string
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		sorted = append(sorted, node)

		for _, dep := range graph[node] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}
	return sorted
}
