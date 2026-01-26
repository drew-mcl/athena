package index

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"unicode"
)

// ExtractSymbols parses a Go file and extracts all symbols.
// filePath should be the absolute path to the file.
// relativePath is the path relative to the project root (used for storage).
func ExtractSymbols(filePath, relativePath string) ([]Symbol, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var symbols []Symbol
	pkgName := file.Name.Name
	symbols = append(symbols, extractSymbolsFromFile(file, fset, relativePath, pkgName)...)

	return symbols, nil
}

func extractSymbolsFromFile(file *ast.File, fset *token.FileSet, filePath, pkgName string) []Symbol {
	var symbols []Symbol
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sym := extractFuncSymbol(d, fset, filePath, pkgName)
			symbols = append(symbols, sym)
		case *ast.GenDecl:
			syms := extractGenDeclSymbols(d, fset, filePath, pkgName)
			symbols = append(symbols, syms...)
		}
	}
	return symbols
}

// extractFuncSymbol extracts a Symbol from a function or method declaration.
func extractFuncSymbol(fn *ast.FuncDecl, fset *token.FileSet, filePath, pkgName string) Symbol {
	sym := Symbol{
		Name:       fn.Name.Name,
		FilePath:   filePath,
		LineNumber: fset.Position(fn.Pos()).Line,
		Package:    pkgName,
		Exported:   isExported(fn.Name.Name),
	}

	// Check if it's a method (has receiver)
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		sym.Kind = SymbolKindMethod
		sym.Receiver = formatReceiver(fn.Recv.List[0].Type)
	} else {
		sym.Kind = SymbolKindFunc
	}

	return sym
}

// extractGenDeclSymbols extracts symbols from a general declaration (type, const, var).
func extractGenDeclSymbols(gd *ast.GenDecl, fset *token.FileSet, filePath, pkgName string) []Symbol {
	var symbols []Symbol

	for _, spec := range gd.Specs {
		symbols = append(symbols, extractSpecSymbols(spec, gd, fset, filePath, pkgName)...)
	}

	return symbols
}

func extractSpecSymbols(spec ast.Spec, gd *ast.GenDecl, fset *token.FileSet, filePath, pkgName string) []Symbol {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		return []Symbol{extractTypeSymbol(s, fset, filePath, pkgName)}
	case *ast.ValueSpec:
		return extractValueSymbols(s, gd, fset, filePath, pkgName)
	default:
		return nil
	}
}

func extractTypeSymbol(spec *ast.TypeSpec, fset *token.FileSet, filePath, pkgName string) Symbol {
	sym := Symbol{
		Name:       spec.Name.Name,
		FilePath:   filePath,
		LineNumber: fset.Position(spec.Pos()).Line,
		Package:    pkgName,
		Exported:   isExported(spec.Name.Name),
	}

	switch spec.Type.(type) {
	case *ast.StructType:
		sym.Kind = SymbolKindStruct
	case *ast.InterfaceType:
		sym.Kind = SymbolKindInterface
	default:
		sym.Kind = SymbolKindType
	}

	return sym
}

func extractValueSymbols(spec *ast.ValueSpec, gd *ast.GenDecl, fset *token.FileSet, filePath, pkgName string) []Symbol {
	kind := SymbolKindVar
	if gd.Tok == token.CONST {
		kind = SymbolKindConst
	}

	var symbols []Symbol
	for _, name := range spec.Names {
		if name.Name == "_" {
			continue
		}
		symbols = append(symbols, Symbol{
			Name:       name.Name,
			Kind:       kind,
			FilePath:   filePath,
			LineNumber: fset.Position(name.Pos()).Line,
			Package:    pkgName,
			Exported:   isExported(name.Name),
		})
	}
	return symbols
}

// formatReceiver converts a receiver type expression to a string.
func formatReceiver(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		// Pointer receiver: *Daemon
		return "*" + formatReceiver(t.X)
	case *ast.Ident:
		// Value receiver: Daemon
		return t.Name
	case *ast.IndexExpr:
		// Generic receiver: Daemon[T]
		return formatReceiver(t.X) + "[" + formatReceiver(t.Index) + "]"
	case *ast.IndexListExpr:
		// Multi-param generic: Daemon[K, V]
		indices := make([]string, len(t.Indices))
		for i, idx := range t.Indices {
			indices[i] = formatReceiver(idx)
		}
		return formatReceiver(t.X) + "[" + strings.Join(indices, ", ") + "]"
	default:
		return ""
	}
}

// isExported returns true if the name starts with an uppercase letter.
func isExported(name string) bool {
	if name == "" {
		return false
	}
	r := []rune(name)
	return unicode.IsUpper(r[0])
}

// ExtractSymbolsFromDir parses all Go files in a directory and extracts symbols.
// It does not recurse into subdirectories.
func ExtractSymbolsFromDir(dirPath, relativeDir string) ([]Symbol, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dirPath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var symbols []Symbol

	for _, pkg := range pkgs {
		// Skip test packages
		if strings.HasSuffix(pkg.Name, "_test") {
			continue
		}

		symbols = appendPackageSymbols(symbols, pkg, fset, relativeDir)
	}

	return symbols, nil
}

func appendPackageSymbols(symbols []Symbol, pkg *ast.Package, fset *token.FileSet, relativeDir string) []Symbol {
	for fileName, file := range pkg.Files {
		if strings.HasSuffix(fileName, testFileSuffix) {
			continue
		}
		relativePath := filepath.Join(relativeDir, filepath.Base(fileName))
		pkgName := file.Name.Name
		symbols = append(symbols, extractSymbolsFromFile(file, fset, relativePath, pkgName)...)
	}
	return symbols
}
