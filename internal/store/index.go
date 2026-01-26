package store

import (
	"database/sql"

	"github.com/drewfead/athena/internal/index"
)

// SaveIndex persists an entire Index to the database.
// This replaces any existing data for the given project hash.
func (s *Store) SaveIndex(idx *index.Index) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.clearIndexData(tx, idx.ProjectHash); err != nil {
		return err
	}
	if err := s.insertSymbols(tx, idx); err != nil {
		return err
	}
	if err := s.insertDependencies(tx, idx); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) clearIndexData(tx *sql.Tx, projectHash string) error {
	if _, err := tx.Exec("DELETE FROM symbols WHERE project_hash = ?", projectHash); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM dependencies WHERE project_hash = ?", projectHash); err != nil {
		return err
	}
	return nil
}

func (s *Store) insertSymbols(tx *sql.Tx, idx *index.Index) error {
	symbolStmt, err := tx.Prepare(`
		INSERT INTO symbols (project_hash, symbol_name, kind, file_path, line_number, package, receiver, exported)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer symbolStmt.Close()

	for _, symbols := range idx.FileSymbols {
		for _, sym := range symbols {
			_, err := symbolStmt.Exec(
				idx.ProjectHash,
				sym.Name,
				string(sym.Kind),
				sym.FilePath,
				sym.LineNumber,
				sym.Package,
				sym.Receiver,
				sym.Exported,
			)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) insertDependencies(tx *sql.Tx, idx *index.Index) error {
	depStmt, err := tx.Prepare(`
		INSERT INTO dependencies (project_hash, from_file, to_file, dep_type, import_path, is_internal)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer depStmt.Close()

	for _, deps := range idx.Deps {
		for _, dep := range deps {
			_, err := depStmt.Exec(
				idx.ProjectHash,
				dep.FromFile,
				dep.ToFile,
				string(dep.DepType),
				dep.ImportPath,
				dep.IsInternal,
			)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// LoadIndex loads an Index from the database by project hash.
// Returns nil if no index exists for the given hash.
func (s *Store) LoadIndex(projectHash string) (*index.Index, error) {
	hasData, err := s.indexExists(projectHash)
	if err != nil {
		return nil, err
	}
	if !hasData {
		return nil, nil
	}

	idx := index.NewIndex("", projectHash, "")

	if err := s.loadIndexSymbols(idx, projectHash); err != nil {
		return nil, err
	}
	if err := s.loadIndexDependencies(idx, projectHash); err != nil {
		return nil, err
	}

	return idx, nil
}

func (s *Store) indexExists(projectHash string) (bool, error) {
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM symbols WHERE project_hash = ?", projectHash).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Store) loadIndexSymbols(idx *index.Index, projectHash string) error {
	rows, err := s.db.Query(`
		SELECT symbol_name, kind, file_path, line_number, package, receiver, exported
		FROM symbols WHERE project_hash = ?
	`, projectHash)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var sym index.Symbol
		var kind string
		var pkg, receiver sql.NullString
		if err := rows.Scan(&sym.Name, &kind, &sym.FilePath, &sym.LineNumber, &pkg, &receiver, &sym.Exported); err != nil {
			return err
		}
		sym.Kind = index.SymbolKind(kind)
		if pkg.Valid {
			sym.Package = pkg.String
		}
		if receiver.Valid {
			sym.Receiver = receiver.String
		}
		idx.AddSymbol(sym)
	}
	return rows.Err()
}

func (s *Store) loadIndexDependencies(idx *index.Index, projectHash string) error {
	depRows, err := s.db.Query(`
		SELECT from_file, to_file, dep_type, import_path, is_internal
		FROM dependencies WHERE project_hash = ?
	`, projectHash)
	if err != nil {
		return err
	}
	defer depRows.Close()

	for depRows.Next() {
		var dep index.Dependency
		var depType string
		var importPath sql.NullString
		if err := depRows.Scan(&dep.FromFile, &dep.ToFile, &depType, &importPath, &dep.IsInternal); err != nil {
			return err
		}
		dep.DepType = index.DepType(depType)
		if importPath.Valid {
			dep.ImportPath = importPath.String
		}
		idx.AddDependency(dep)
	}
	return depRows.Err()
}

// LookupSymbol queries the database for a symbol by name.
func (s *Store) LookupSymbol(projectHash, name string) ([]index.Symbol, error) {
	rows, err := s.db.Query(`
		SELECT symbol_name, kind, file_path, line_number, package, receiver, exported
		FROM symbols WHERE project_hash = ? AND symbol_name = ?
	`, projectHash, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var symbols []index.Symbol
	for rows.Next() {
		var sym index.Symbol
		var kind string
		var pkg, receiver sql.NullString
		if err := rows.Scan(&sym.Name, &kind, &sym.FilePath, &sym.LineNumber, &pkg, &receiver, &sym.Exported); err != nil {
			return nil, err
		}
		sym.Kind = index.SymbolKind(kind)
		if pkg.Valid {
			sym.Package = pkg.String
		}
		if receiver.Valid {
			sym.Receiver = receiver.String
		}
		symbols = append(symbols, sym)
	}
	return symbols, rows.Err()
}

// GetFileSymbols queries the database for all symbols in a file.
func (s *Store) GetFileSymbols(projectHash, filePath string) ([]index.Symbol, error) {
	rows, err := s.db.Query(`
		SELECT symbol_name, kind, file_path, line_number, package, receiver, exported
		FROM symbols WHERE project_hash = ? AND file_path = ?
		ORDER BY line_number
	`, projectHash, filePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var symbols []index.Symbol
	for rows.Next() {
		var sym index.Symbol
		var kind string
		var pkg, receiver sql.NullString
		if err := rows.Scan(&sym.Name, &kind, &sym.FilePath, &sym.LineNumber, &pkg, &receiver, &sym.Exported); err != nil {
			return nil, err
		}
		sym.Kind = index.SymbolKind(kind)
		if pkg.Valid {
			sym.Package = pkg.String
		}
		if receiver.Valid {
			sym.Receiver = receiver.String
		}
		symbols = append(symbols, sym)
	}
	return symbols, rows.Err()
}

// GetFileDependencies returns files that the given file depends on.
func (s *Store) GetFileDependencies(projectHash, filePath string) ([]index.Dependency, error) {
	rows, err := s.db.Query(`
		SELECT from_file, to_file, dep_type, import_path, is_internal
		FROM dependencies WHERE project_hash = ? AND from_file = ?
	`, projectHash, filePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deps []index.Dependency
	for rows.Next() {
		var dep index.Dependency
		var depType string
		var importPath sql.NullString
		if err := rows.Scan(&dep.FromFile, &dep.ToFile, &depType, &importPath, &dep.IsInternal); err != nil {
			return nil, err
		}
		dep.DepType = index.DepType(depType)
		if importPath.Valid {
			dep.ImportPath = importPath.String
		}
		deps = append(deps, dep)
	}
	return deps, rows.Err()
}

// GetFileDependents returns files that depend on the given file/package.
func (s *Store) GetFileDependents(projectHash, filePath string) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT DISTINCT from_file
		FROM dependencies WHERE project_hash = ? AND to_file = ?
	`, projectHash, filePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []string
	for rows.Next() {
		var file string
		if err := rows.Scan(&file); err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, rows.Err()
}

// SearchSymbols searches for symbols matching a prefix pattern.
func (s *Store) SearchSymbols(projectHash, pattern string) ([]index.Symbol, error) {
	rows, err := s.db.Query(`
		SELECT symbol_name, kind, file_path, line_number, package, receiver, exported
		FROM symbols WHERE project_hash = ? AND symbol_name LIKE ?
		ORDER BY symbol_name, file_path
		LIMIT 100
	`, projectHash, pattern+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var symbols []index.Symbol
	for rows.Next() {
		var sym index.Symbol
		var kind string
		var pkg, receiver sql.NullString
		if err := rows.Scan(&sym.Name, &kind, &sym.FilePath, &sym.LineNumber, &pkg, &receiver, &sym.Exported); err != nil {
			return nil, err
		}
		sym.Kind = index.SymbolKind(kind)
		if pkg.Valid {
			sym.Package = pkg.String
		}
		if receiver.Valid {
			sym.Receiver = receiver.String
		}
		symbols = append(symbols, sym)
	}
	return symbols, rows.Err()
}

// GetSymbolsByKind returns all symbols of a specific kind in a project.
func (s *Store) GetSymbolsByKind(projectHash string, kind index.SymbolKind) ([]index.Symbol, error) {
	rows, err := s.db.Query(`
		SELECT symbol_name, kind, file_path, line_number, package, receiver, exported
		FROM symbols WHERE project_hash = ? AND kind = ?
		ORDER BY symbol_name
	`, projectHash, string(kind))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var symbols []index.Symbol
	for rows.Next() {
		var sym index.Symbol
		var k string
		var pkg, receiver sql.NullString
		if err := rows.Scan(&sym.Name, &k, &sym.FilePath, &sym.LineNumber, &pkg, &receiver, &sym.Exported); err != nil {
			return nil, err
		}
		sym.Kind = index.SymbolKind(k)
		if pkg.Valid {
			sym.Package = pkg.String
		}
		if receiver.Valid {
			sym.Receiver = receiver.String
		}
		symbols = append(symbols, sym)
	}
	return symbols, rows.Err()
}

// GetIndexStats returns statistics about the stored index.
func (s *Store) GetIndexStats(projectHash string) (index.IndexStats, error) {
	var stats index.IndexStats

	// Total symbols
	err := s.db.QueryRow("SELECT COUNT(*) FROM symbols WHERE project_hash = ?", projectHash).Scan(&stats.TotalSymbols)
	if err != nil {
		return stats, err
	}

	// Unique names
	err = s.db.QueryRow("SELECT COUNT(DISTINCT symbol_name) FROM symbols WHERE project_hash = ?", projectHash).Scan(&stats.UniqueNames)
	if err != nil {
		return stats, err
	}

	// Total files
	err = s.db.QueryRow("SELECT COUNT(DISTINCT file_path) FROM symbols WHERE project_hash = ?", projectHash).Scan(&stats.TotalFiles)
	if err != nil {
		return stats, err
	}

	// Total dependencies
	err = s.db.QueryRow("SELECT COUNT(*) FROM dependencies WHERE project_hash = ?", projectHash).Scan(&stats.TotalDeps)
	if err != nil {
		return stats, err
	}

	return stats, nil
}

// DeleteIndex removes all index data for a project hash.
func (s *Store) DeleteIndex(projectHash string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM symbols WHERE project_hash = ?", projectHash); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM dependencies WHERE project_hash = ?", projectHash); err != nil {
		return err
	}

	return tx.Commit()
}

// ListProjectHashes returns all project hashes that have been indexed.
func (s *Store) ListProjectHashes() ([]string, error) {
	rows, err := s.db.Query("SELECT DISTINCT project_hash FROM symbols")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hashes []string
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		hashes = append(hashes, hash)
	}
	return hashes, rows.Err()
}
