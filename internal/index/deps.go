package index

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

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

		dep := Dependency{
			FromFile:   relativePath,
			DepType:    DepTypeImport,
			ImportPath: importPath,
			IsInternal: strings.HasPrefix(importPath, modulePath),
		}

		// For internal imports, try to determine the target file path
		if dep.IsInternal {
			// Convert import path to relative directory path
			// e.g., "github.com/drewfead/athena/internal/store" -> "internal/store"
			relImportPath := strings.TrimPrefix(importPath, modulePath)
			relImportPath = strings.TrimPrefix(relImportPath, "/")
			dep.ToFile = relImportPath // Store as directory path
		}

		deps = append(deps, dep)
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

		for fileName, file := range pkg.Files {
			// Skip test files
			if strings.HasSuffix(fileName, "_test.go") {
				continue
			}

			relativePath := filepath.Join(relativeDir, filepath.Base(fileName))

			for _, imp := range file.Imports {
				importPath := strings.Trim(imp.Path.Value, `"`)

				dep := Dependency{
					FromFile:   relativePath,
					DepType:    DepTypeImport,
					ImportPath: importPath,
					IsInternal: strings.HasPrefix(importPath, modulePath),
				}

				// For internal imports, determine the target directory path
				if dep.IsInternal {
					relImportPath := strings.TrimPrefix(importPath, modulePath)
					relImportPath = strings.TrimPrefix(relImportPath, "/")
					dep.ToFile = relImportPath
				}

				deps = append(deps, dep)
			}
		}
	}

	return deps, nil
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
		if dep.IsInternal && dep.ToFile != "" {
			// Get the directory of the source file
			sourceDir := filepath.Dir(dep.FromFile)
			targetDir := dep.ToFile

			// Skip self-references
			if sourceDir == targetDir {
				continue
			}

			// Add edge: sourceDir depends on targetDir
			existing := graph[sourceDir]
			found := false
			for _, e := range existing {
				if e == targetDir {
					found = true
					break
				}
			}
			if !found {
				graph[sourceDir] = append(existing, targetDir)
			}
		}
	}

	return graph
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
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
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
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
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
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
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
	// Calculate in-degree for each node
	inDegree := make(map[string]int)
	allNodes := make(map[string]bool)

	// Initialize all nodes
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

	// Ensure all nodes have an in-degree entry
	for node := range allNodes {
		if _, ok := inDegree[node]; !ok {
			inDegree[node] = 0
		}
	}

	// Queue of nodes with in-degree 0
	var queue []string
	for node, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, node)
		}
	}

	var sorted []string

	for len(queue) > 0 {
		// Pop from queue
		node := queue[0]
		queue = queue[1:]
		sorted = append(sorted, node)

		// Reduce in-degree of dependent nodes
		for _, dep := range graph[node] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	return sorted, nil
}
