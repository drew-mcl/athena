package store

import "testing"

// seedQueueItems creates worktrees and queue items for merge queue tests.
// Returns (paths, branches) for the 3 items created.
func seedQueueItems(t *testing.T, st *Store, project string, pathPrefix string, idPrefix string) ([]string, []string) {
	t.Helper()

	paths := []string{
		"/tmp/" + pathPrefix + "-a",
		"/tmp/" + pathPrefix + "-b",
		"/tmp/" + pathPrefix + "-c",
	}
	branches := []string{"feat/a", "feat/b", "feat/c"}

	for i := range paths {
		if err := st.UpsertWorktree(&Worktree{
			Path:    paths[i],
			Project: project,
			Branch:  branches[i],
			IsMain:  false,
			Status:  WorktreeStatusActive,
		}); err != nil {
			t.Fatalf("failed to seed worktree %d: %v", i, err)
		}
	}

	items := []*MergeQueueItem{
		{
			ID:           idPrefix + "a",
			Project:      project,
			WorktreePath: paths[0],
			Branch:       branches[0],
			BaseBranch:   "main",
			BaseCommit:   "main0",
			HeadCommit:   "a1",
		},
		{
			ID:           idPrefix + "b",
			Project:      project,
			WorktreePath: paths[1],
			Branch:       branches[1],
			BaseBranch:   branches[0],
			BaseCommit:   "a1",
			HeadCommit:   "b1",
		},
		{
			ID:           idPrefix + "c",
			Project:      project,
			WorktreePath: paths[2],
			Branch:       branches[2],
			BaseBranch:   branches[1],
			BaseCommit:   "b1",
			HeadCommit:   "c1",
		},
	}
	for _, item := range items {
		if err := st.AddToMergeQueue(item); err != nil {
			t.Fatalf("failed to add queue item %s: %v", item.ID, err)
		}
	}

	return paths, branches
}

func TestMoveToBackOfQueueKeepsPositionAndMarksDiverged(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	paths, _ := seedQueueItems(t, st, "proj", "proj", "q")

	if err := st.MoveToBackOfQueue(paths[0], "a2"); err != nil {
		t.Fatalf("MoveToBackOfQueue failed: %v", err)
	}

	items, err := st.GetMergeQueue("proj")
	if err != nil {
		t.Fatalf("GetMergeQueue failed: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 queue items, got %d", len(items))
	}

	if items[0].Position != 1 || items[0].WorktreePath != paths[0] {
		t.Fatalf("first item moved unexpectedly: pos=%d path=%s", items[0].Position, items[0].WorktreePath)
	}
	if items[0].HeadCommit != "a2" {
		t.Fatalf("expected updated head commit a2, got %s", items[0].HeadCommit)
	}
	if items[0].Status != MergeQueueStatusQueued {
		t.Fatalf("expected first item queued, got %s", items[0].Status)
	}

	if items[1].Position != 2 || items[1].Status != MergeQueueStatusDiverged {
		t.Fatalf("expected second item diverged at pos2, got status=%s pos=%d", items[1].Status, items[1].Position)
	}
	if items[2].Position != 3 || items[2].Status != MergeQueueStatusDiverged {
		t.Fatalf("expected third item diverged at pos3, got status=%s pos=%d", items[2].Status, items[2].Position)
	}
}

func TestGetQueueHeadStopsAtDivergence(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	paths, branches := seedQueueItems(t, st, "proj", "head", "h")

	if err := st.UpdateMergeQueueItem(paths[1], MergeQueueStatusDiverged, ""); err != nil {
		t.Fatalf("failed to mark diverged: %v", err)
	}

	branch, commit, err := st.GetQueueHead("proj")
	if err != nil {
		t.Fatalf("GetQueueHead failed: %v", err)
	}
	if branch != branches[0] || commit != "a1" {
		t.Fatalf("expected head to stop at first item (%s,a1), got (%s,%s)", branches[0], branch, commit)
	}
}

func TestMarkQueueItemRebasedWithBasePersistsMetadata(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	project := "proj"
	path := "/tmp/rebase-a"
	branch := "feat/a"

	if err := st.UpsertWorktree(&Worktree{
		Path:    path,
		Project: project,
		Branch:  branch,
		IsMain:  false,
		Status:  WorktreeStatusActive,
	}); err != nil {
		t.Fatalf("failed to seed worktree: %v", err)
	}

	if err := st.AddToMergeQueue(&MergeQueueItem{
		ID:           "ra",
		Project:      project,
		WorktreePath: path,
		Branch:       branch,
		BaseBranch:   "main",
		BaseCommit:   "main0",
		HeadCommit:   "a1",
	}); err != nil {
		t.Fatalf("failed to add queue item: %v", err)
	}

	if err := st.MarkQueueItemRebasedWithBase(path, "feat/root", "r1", "a2"); err != nil {
		t.Fatalf("MarkQueueItemRebasedWithBase failed: %v", err)
	}

	item, err := st.GetMergeQueueItem(path)
	if err != nil {
		t.Fatalf("GetMergeQueueItem failed: %v", err)
	}
	if item == nil {
		t.Fatal("expected queue item")
	}
	if item.Status != MergeQueueStatusQueued {
		t.Fatalf("status = %s, want queued", item.Status)
	}
	if item.BaseBranch != "feat/root" {
		t.Fatalf("base branch = %s, want feat/root", item.BaseBranch)
	}
	if item.BaseCommit != "r1" {
		t.Fatalf("base commit = %s, want r1", item.BaseCommit)
	}
	if item.HeadCommit != "a2" {
		t.Fatalf("head commit = %s, want a2", item.HeadCommit)
	}
}
