package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drewfead/athena/internal/executil"
	"github.com/drewfead/athena/internal/store"
)

func TestSelectQueueRebaseTarget(t *testing.T) {
	prev := &store.MergeQueueItem{
		Position:   1,
		Branch:     "feat/a",
		HeadCommit: "abc123",
	}
	item := &store.MergeQueueItem{Position: 2}
	positionMap := map[int]*store.MergeQueueItem{1: prev}

	ref, baseCommit, baseBranch, ok := selectQueueRebaseTarget(item, positionMap)
	if !ok {
		t.Fatal("expected previous queue target")
	}
	if ref != "abc123" {
		t.Fatalf("ref = %q, want abc123", ref)
	}
	if baseCommit != "abc123" {
		t.Fatalf("baseCommit = %q, want abc123", baseCommit)
	}
	if baseBranch != "feat/a" {
		t.Fatalf("baseBranch = %q, want feat/a", baseBranch)
	}
}

func TestSelectQueueRebaseTargetFallsBackToBranchWhenHeadMissing(t *testing.T) {
	prev := &store.MergeQueueItem{
		Position: 1,
		Branch:   "feat/a",
	}
	item := &store.MergeQueueItem{Position: 2}
	positionMap := map[int]*store.MergeQueueItem{1: prev}

	ref, baseCommit, baseBranch, ok := selectQueueRebaseTarget(item, positionMap)
	if !ok {
		t.Fatal("expected previous queue target")
	}
	if ref != "feat/a" {
		t.Fatalf("ref = %q, want feat/a", ref)
	}
	if baseCommit != "" {
		t.Fatalf("baseCommit = %q, want empty", baseCommit)
	}
	if baseBranch != "feat/a" {
		t.Fatalf("baseBranch = %q, want feat/a", baseBranch)
	}
}

func TestSelectQueueRebaseTargetRejectsUnstablePredecessor(t *testing.T) {
	prev := &store.MergeQueueItem{
		Position:   1,
		Branch:     "feat/a",
		HeadCommit: "abc123",
		Status:     store.MergeQueueStatusConflict,
	}
	item := &store.MergeQueueItem{Position: 2}
	positionMap := map[int]*store.MergeQueueItem{1: prev}

	if _, _, _, ok := selectQueueRebaseTarget(item, positionMap); ok {
		t.Fatal("expected unstable predecessor to be rejected")
	}
}

func TestResolveQueueRebaseTargetRejectsDownstreamWithoutStablePredecessor(t *testing.T) {
	item := &store.MergeQueueItem{
		Position:     2,
		WorktreePath: t.TempDir(),
	}
	positionMap := map[int]*store.MergeQueueItem{
		1: {
			Position:   1,
			Branch:     "feat/a",
			HeadCommit: "abc123",
			Status:     store.MergeQueueStatusConflict,
		},
	}

	_, _, _, err := resolveQueueRebaseTarget(item, positionMap)
	if err == nil {
		t.Fatal("expected error for downstream item without stable predecessor")
	}
	if !strings.Contains(err.Error(), "no stable predecessor") {
		t.Fatalf("error = %v, want no stable predecessor", err)
	}
}

func TestResolveQueueRebaseTargetRootFallsBackToDefaultBase(t *testing.T) {
	repo := initTestRepo(t, "main")
	item := &store.MergeQueueItem{
		Position:     1,
		WorktreePath: repo,
	}

	ref, baseCommit, baseBranch, err := resolveQueueRebaseTarget(item, map[int]*store.MergeQueueItem{})
	if err != nil {
		t.Fatalf("resolveQueueRebaseTarget() error = %v", err)
	}
	if ref != "main" {
		t.Fatalf("ref = %q, want main", ref)
	}
	if baseBranch != "main" {
		t.Fatalf("baseBranch = %q, want main", baseBranch)
	}
	if baseCommit == "" {
		t.Fatal("expected non-empty base commit")
	}
}

func TestGetGitDefaultBaseRefUsesOriginHead(t *testing.T) {
	repo := initTestRepo(t, "main")
	head := runGit(t, repo, "rev-parse", "HEAD")

	runGit(t, repo, "update-ref", "refs/remotes/origin/main", head)
	runGit(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	ref, branch, err := getGitDefaultBaseRef(repo)
	if err != nil {
		t.Fatalf("getGitDefaultBaseRef() error = %v", err)
	}
	if ref != "origin/main" {
		t.Fatalf("ref = %q, want origin/main", ref)
	}
	if branch != "main" {
		t.Fatalf("branch = %q, want main", branch)
	}
}

func TestGetGitDefaultBaseRefFallsBackToMainAndMaster(t *testing.T) {
	mainRepo := initTestRepo(t, "main")
	ref, branch, err := getGitDefaultBaseRef(mainRepo)
	if err != nil {
		t.Fatalf("main fallback error = %v", err)
	}
	if ref != "main" || branch != "main" {
		t.Fatalf("main fallback got (%q,%q), want (main,main)", ref, branch)
	}

	masterRepo := initTestRepo(t, "master")
	ref, branch, err = getGitDefaultBaseRef(masterRepo)
	if err != nil {
		t.Fatalf("master fallback error = %v", err)
	}
	if ref != "master" || branch != "master" {
		t.Fatalf("master fallback got (%q,%q), want (master,master)", ref, branch)
	}
}

func initTestRepo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()

	if _, err := runGitErr("", "init", "-b", branch, dir); err != nil {
		runGit(t, "", "init", dir)
		runGit(t, dir, "checkout", "-B", branch)
	}
	runGit(t, dir, "config", "user.name", "test")
	runGit(t, dir, "config", "user.email", "test@example.com")

	file := filepath.Join(dir, "README.md")
	if err := os.WriteFile(file, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "init")

	return dir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runGitErr(dir, args...)
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return out
}

func runGitErr(dir string, args ...string) (string, error) {
	cmd, err := executil.Command("git", args...)
	if err != nil {
		return "", err
	}
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
