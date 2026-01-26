// Package index provides codebase indexing and relevance scoring for context optimization.
package index

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// RelevanceSignals captures the factors contributing to a file's relevance score.
type RelevanceSignals struct {
	PathMatch       float64 // Score for keyword matches in file path
	SymbolMatch     float64 // Score for keyword matches against symbols defined in file
	DependencyHops  int     // Distance from highest-scoring files in dependency graph
	RecentlyChanged bool    // Whether the file has recent git activity
	FileSize        int     // File size in bytes (smaller is generally better)
}

// ScoredFile represents a file with its computed relevance score.
type ScoredFile struct {
	Path    string           // Absolute path to the file
	Score   float64          // Computed relevance score (0-1, higher is better)
	Signals RelevanceSignals // The individual signals that contributed to the score
	Reason  string           // Human-readable explanation of why this file scored high
}

// Scorer computes file relevance scores for a given task description.
type Scorer struct {
	index *Index // Symbol index and dependency graph (may be nil if not built)
}

// NewScorer creates a new Scorer.
// If index is nil, scoring will be based on path matching and file size only.
func NewScorer(index *Index) *Scorer {
	return &Scorer{
		index: index,
	}
}

// ScoreFiles returns files ranked by relevance to the task.
// It extracts keywords from the task, scores each file, and returns the top N results.
func (s *Scorer) ScoreFiles(task string, projectPath string, limit int) []ScoredFile {
	if limit <= 0 {
		limit = 20 // Default to top 20 files
	}

	// Extract keywords from task
	keywords := ExtractKeywords(task)
	if len(keywords) == 0 {
		return nil
	}

	// Find all Go files in the project
	files := s.findGoFiles(projectPath)
	if len(files) == 0 {
		return nil
	}

	// Score each file against keywords
	scored := make([]ScoredFile, 0, len(files))
	for _, file := range files {
		sf := s.scoreFile(file, projectPath, keywords)
		if sf.Score > 0 {
			scored = append(scored, sf)
		}
	}

	// Sort by score (descending)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	// Apply dependency boost: files closer to high-scoring files get a bonus
	s.applyDependencyBoost(scored, projectPath)

	// Re-sort after dependency boost
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	// Return top N
	if len(scored) > limit {
		scored = scored[:limit]
	}

	return scored
}

// scoreFile computes the relevance score for a single file.
func (s *Scorer) scoreFile(filePath, projectPath string, keywords []string) ScoredFile {
	signals := RelevanceSignals{}
	var reasons []string

	// Compute relative path for matching
	relPath, _ := filepath.Rel(projectPath, filePath)
	if relPath == "" {
		relPath = filePath
	}

	// 1. Path matching
	pathScore := s.computePathScore(relPath, keywords)
	signals.PathMatch = pathScore
	if pathScore > 0 {
		reasons = append(reasons, "path matches keywords")
	}

	// 2. Symbol matching (if index available)
	symbolScore := s.computeSymbolScore(filePath, keywords)
	signals.SymbolMatch = symbolScore
	if symbolScore > 0 {
		reasons = append(reasons, "defines matching symbols")
	}

	// 3. File size penalty
	fileInfo, err := os.Stat(filePath)
	if err == nil {
		signals.FileSize = int(fileInfo.Size())
	}
	sizePenalty := s.computeSizePenalty(signals.FileSize)

	// 4. Recently changed (placeholder - would need git integration)
	signals.RecentlyChanged = false // Placeholder until git metadata is wired in.

	// Compute final score using the formula:
	// score = (pathMatch * 0.3) + (symbolMatch * 0.5) + (depBonus * 0.2) - (sizePenalty * 0.1)
	// Note: depBonus is applied in applyDependencyBoost after initial scoring
	score := (pathScore * 0.3) + (symbolScore * 0.5) - (sizePenalty * 0.1)

	// Clamp score to [0, 1]
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	reason := strings.Join(reasons, "; ")
	if reason == "" {
		reason = "low relevance"
	}

	return ScoredFile{
		Path:    filePath,
		Score:   score,
		Signals: signals,
		Reason:  reason,
	}
}

// computePathScore scores how well a file path matches the keywords.
func (s *Scorer) computePathScore(path string, keywords []string) float64 {
	lowerPath := strings.ToLower(path)

	// Split path into components for matching
	pathParts := strings.FieldsFunc(lowerPath, func(r rune) bool {
		return r == '/' || r == '_' || r == '-' || r == '.'
	})

	var matchCount int
	for _, kw := range keywords {
		kw = strings.ToLower(kw)

		// Check if keyword appears in path
		if strings.Contains(lowerPath, kw) {
			matchCount++
			continue
		}

		// Check if keyword matches any path component
		for _, part := range pathParts {
			if part == kw || strings.HasPrefix(part, kw) || strings.HasSuffix(part, kw) {
				matchCount++
				break
			}
		}
	}

	if len(keywords) == 0 {
		return 0
	}

	// Return ratio of matched keywords
	return float64(matchCount) / float64(len(keywords))
}

// computeSymbolScore scores how well file symbols match the keywords.
func (s *Scorer) computeSymbolScore(filePath string, keywords []string) float64 {
	if s.index == nil || len(keywords) == 0 {
		return 0
	}

	symbols := s.index.GetFileSymbols(filePath)
	if len(symbols) == 0 {
		return 0
	}

	matchCount := countSymbolMatches(symbols, keywords)
	score := float64(matchCount) / float64(len(keywords)*2)
	if score > 1 {
		score = 1
	}
	return score
}

func countSymbolMatches(symbols []Symbol, keywords []string) int {
	matchCount := 0
	for _, keyword := range keywords {
		matchCount += scoreKeywordAgainstSymbols(keyword, symbols)
	}
	return matchCount
}

func scoreKeywordAgainstSymbols(keyword string, symbols []Symbol) int {
	kwLower := strings.ToLower(keyword)
	for _, sym := range symbols {
		symLower := strings.ToLower(sym.Name)
		if symLower == kwLower {
			return 2
		}
		if strings.Contains(symLower, kwLower) {
			return 1
		}
		if identifierPartMatch(sym.Name, kwLower) {
			return 1
		}
	}
	return 0
}

func identifierPartMatch(symbolName, keywordLower string) bool {
	for _, part := range splitIdentifier(symbolName) {
		if strings.ToLower(part) == keywordLower {
			return true
		}
	}
	return false
}

// computeSizePenalty returns a penalty score based on file size.
// Larger files get higher penalties (up to 1.0).
func (s *Scorer) computeSizePenalty(size int) float64 {
	if size <= 0 {
		return 0
	}

	// Files under 5KB: no penalty
	// Files 5KB-20KB: linear penalty 0-0.3
	// Files 20KB-100KB: linear penalty 0.3-0.7
	// Files over 100KB: penalty 0.7-1.0

	const (
		small  = 5 * 1024
		medium = 20 * 1024
		large  = 100 * 1024
	)

	switch {
	case size <= small:
		return 0
	case size <= medium:
		return 0.3 * float64(size-small) / float64(medium-small)
	case size <= large:
		return 0.3 + 0.4*float64(size-medium)/float64(large-medium)
	default:
		// Cap at 1.0
		penalty := 0.7 + 0.3*float64(size-large)/float64(large)
		if penalty > 1.0 {
			penalty = 1.0
		}
		return penalty
	}
}

// applyDependencyBoost boosts scores of files that are close in the dependency graph
// to the highest-scoring files.
func (s *Scorer) applyDependencyBoost(scored []ScoredFile, projectPath string) {
	if s.index == nil || len(scored) == 0 {
		return
	}

	topFiles := topScoringFiles(scored, 3, 0.3)
	if len(topFiles) == 0 {
		return
	}

	for i := range scored {
		s.applyBoost(&scored[i], topFiles)
	}
}

func topScoringFiles(scored []ScoredFile, limit int, minScore float64) []string {
	topFiles := make([]string, 0, limit)
	for i := 0; i < len(scored) && i < limit; i++ {
		if scored[i].Score > minScore {
			topFiles = append(topFiles, scored[i].Path)
		}
	}
	return topFiles
}

func (s *Scorer) applyBoost(file *ScoredFile, topFiles []string) {
	minHops := s.minDependencyHops(file.Path, topFiles)
	file.Signals.DependencyHops = minHops
	if minHops < 0 {
		return
	}

	depBonus := 1.0 / float64(1+minHops)
	file.Score += depBonus * 0.2

	if minHops <= 2 {
		updateDependencyReason(file)
	}

	if file.Score > 1 {
		file.Score = 1
	}
}

func (s *Scorer) minDependencyHops(path string, topFiles []string) int {
	minHops := -1
	for _, topFile := range topFiles {
		if path == topFile {
			return 0
		}
		hops := s.index.GetDependencyDistance(path, topFile)
		if hops >= 0 && (minHops < 0 || hops < minHops) {
			minHops = hops
		}
	}
	return minHops
}

func updateDependencyReason(file *ScoredFile) {
	if file.Reason != "" && file.Reason != "low relevance" {
		file.Reason += "; close to relevant files"
		return
	}
	file.Reason = "close to relevant files"
}

// findGoFiles returns all .go files in the project directory.
func (s *Scorer) findGoFiles(projectPath string) []string {
	var files []string

	filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Skip hidden directories and common non-source directories
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		// Only include .go files (excluding test files for context)
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, testFileSuffix) {
			files = append(files, path)
		}

		return nil
	})

	return files
}

// ExtractKeywords extracts meaningful keywords from a task description.
// It removes stop words and identifies likely identifiers.
func ExtractKeywords(text string) []string {
	// Common stop words to filter out
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"to": true, "from": true, "in": true, "on": true, "at": true,
		"for": true, "of": true, "and": true, "or": true, "not": true,
		"this": true, "that": true, "it": true, "its": true, "be": true,
		"with": true, "as": true, "by": true, "was": true, "were": true,
		"been": true, "being": true, "have": true, "has": true, "had": true,
		"do": true, "does": true, "did": true, "will": true, "would": true,
		"could": true, "should": true, "may": true, "might": true, "must": true,
		"can": true, "need": true, "want": true, "we": true, "you": true,
		"i": true, "me": true, "my": true, "your": true, "our": true,
		"their": true, "them": true, "they": true, "he": true, "she": true,
		"what": true, "when": true, "where": true, "which": true, "who": true,
		"why": true, "how": true, "all": true, "each": true, "every": true,
		"some": true, "any": true, "no": true, "only": true, "same": true,
		"so": true, "than": true, "too": true, "very": true, "just": true,
		"also": true, "add": true, "fix": true, "update": true, "change": true,
		"make": true, "create": true, "new": true, "like": true, "use": true,
		"using": true, "file": true, "files": true, "code": true, "implement": true,
		"implementation": true, "function": true, "method": true,
	}

	// Extract potential identifiers (CamelCase, snake_case, kebab-case)
	identifierRegex := regexp.MustCompile(`[A-Z][a-z]+(?:[A-Z][a-z]+)*|[a-z]+(?:_[a-z]+)+|[a-z]+(?:-[a-z]+)+`)
	identifiers := identifierRegex.FindAllString(text, -1)

	// Extract regular words
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-'
	})

	// Combine and deduplicate
	seen := make(map[string]bool)
	var keywords []string

	// Add identifiers with higher priority
	for _, id := range identifiers {
		lower := strings.ToLower(id)
		if !stopWords[lower] && len(lower) >= 2 && !seen[lower] {
			seen[lower] = true
			keywords = append(keywords, id) // Keep original case for identifiers
		}
	}

	// Add regular words
	for _, word := range words {
		lower := strings.ToLower(word)
		if !stopWords[lower] && len(lower) >= 2 && !seen[lower] {
			seen[lower] = true
			keywords = append(keywords, word)
		}
	}

	return keywords
}

// splitIdentifier splits a CamelCase or snake_case identifier into parts.
func splitIdentifier(id string) []string {
	// Handle snake_case
	if strings.Contains(id, "_") {
		return strings.Split(id, "_")
	}

	// Handle CamelCase
	var parts []string
	var current strings.Builder

	for i, r := range id {
		if i > 0 && unicode.IsUpper(r) {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
		}
		current.WriteRune(r)
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}
