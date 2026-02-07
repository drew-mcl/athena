package store

import "testing"

func TestMoveToBackOfQueueKeepsPositionAndMarksDiverged(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	project := "proj"
	paths := []string{
		"/tmp/proj-a",
		"/tmp/proj-b",
		"/tmp/proj-c",
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

	seed := []*MergeQueueItem{
		{
			ID:           "qa",
			Project:      project,
			WorktreePath: paths[0],
			Branch:       branches[0],
			BaseBranch:   "main",
			BaseCommit:   "main0",
			HeadCommit:   "a1",
		},
		{
			ID:           "qb",
			Project:      project,
			WorktreePath: paths[1],
			Branch:       branches[1],
			BaseBranch:   branches[0],
			BaseCommit:   "a1",
			HeadCommit:   "b1",
		},
		{
			ID:           "qc",
			Project:      project,
			WorktreePath: paths[2],
			Branch:       branches[2],
			BaseBranch:   branches[1],
			BaseCommit:   "b1",
			HeadCommit:   "c1",
		},
	}
	for _, item := range seed {
		if err := st.AddToMergeQueue(item); err != nil {
			t.Fatalf("failed to add queue item %s: %v", item.ID, err)
		}
	}

	if err := st.MoveToBackOfQueue(paths[0], "a2"); err != nil {
		t.Fatalf("MoveToBackOfQueue failed: %v", err)
	}

	items, err := st.GetMergeQueue(project)
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

	project := "proj"
	paths := []string{
		"/tmp/head-a",
		"/tmp/head-b",
		"/tmp/head-c",
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

	seed := []*MergeQueueItem{
		{
			ID:           "ha",
			Project:      project,
			WorktreePath: paths[0],
			Branch:       branches[0],
			BaseBranch:   "main",
			BaseCommit:   "main0",
			HeadCommit:   "a1",
		},
		{
			ID:           "hb",
			Project:      project,
			WorktreePath: paths[1],
			Branch:       branches[1],
			BaseBranch:   branches[0],
			BaseCommit:   "a1",
			HeadCommit:   "b1",
		},
		{
			ID:           "hc",
			Project:      project,
			WorktreePath: paths[2],
			Branch:       branches[2],
			BaseBranch:   branches[1],
			BaseCommit:   "b1",
			HeadCommit:   "c1",
		},
	}
	for _, item := range seed {
		if err := st.AddToMergeQueue(item); err != nil {
			t.Fatalf("failed to add queue item %s: %v", item.ID, err)
		}
	}

	if err := st.UpdateMergeQueueItem(paths[1], MergeQueueStatusDiverged, ""); err != nil {
		t.Fatalf("failed to mark diverged: %v", err)
	}

	branch, commit, err := st.GetQueueHead(project)
	if err != nil {
		t.Fatalf("GetQueueHead failed: %v", err)
	}
	if branch != branches[0] || commit != "a1" {
		t.Fatalf("expected head to stop at first item (%s,a1), got (%s,%s)", branches[0], branch, commit)
	}
}
