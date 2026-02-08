package daemon

import (
	"context"
	"testing"

	"github.com/drewfead/athena/internal/plugin"
	"github.com/drewfead/athena/internal/plugin/vcs"
)

type fakeVCSProvider struct {
	*vcs.BaseVCS
}

func newFakeVCSProvider(name string) *fakeVCSProvider {
	p := &fakeVCSProvider{BaseVCS: vcs.NewBaseVCS(name)}
	p.SetEnabled(true)
	return p
}

func (f *fakeVCSProvider) GetPR(ctx context.Context, repo, branch string) (*vcs.PullRequest, error) {
	return &vcs.PullRequest{}, nil
}

func (f *fakeVCSProvider) ListOpenPRs(ctx context.Context, repo string) ([]*vcs.PullRequest, error) {
	return nil, nil
}

func (f *fakeVCSProvider) GetPRState(ctx context.Context, repo string, prNumber int) (vcs.PRState, error) {
	return vcs.PRStateOpen, nil
}

func (f *fakeVCSProvider) GetMergeCommit(ctx context.Context, repo string, prNumber int) (string, error) {
	return "", nil
}

func (f *fakeVCSProvider) GetCIStatus(ctx context.Context, repo, branch string) (*vcs.CIRun, error) {
	return nil, nil
}

func (f *fakeVCSProvider) ListCIRuns(ctx context.Context, repo, branch string, limit int) ([]*vcs.CIRun, error) {
	return nil, nil
}

func TestParseRepoFromPRURL(t *testing.T) {
	t.Run("github", func(t *testing.T) {
		provider, repo, ok := parseRepoFromPRURL("https://github.com/drew-mcl/athena/pull/51")
		if !ok {
			t.Fatal("expected parse to succeed")
		}
		if provider != "github" {
			t.Fatalf("provider = %q, want github", provider)
		}
		if repo != "drew-mcl/athena" {
			t.Fatalf("repo = %q, want drew-mcl/athena", repo)
		}
	})

	t.Run("github enterprise style host", func(t *testing.T) {
		provider, repo, ok := parseRepoFromPRURL("https://github.internal.example/org/repo/pull/9")
		if !ok {
			t.Fatal("expected parse to succeed")
		}
		if provider != "github" {
			t.Fatalf("provider = %q, want github", provider)
		}
		if repo != "org/repo" {
			t.Fatalf("repo = %q, want org/repo", repo)
		}
	})

	t.Run("gitlab with subgroup", func(t *testing.T) {
		provider, repo, ok := parseRepoFromPRURL("https://gitlab.com/group/subgroup/athena/-/merge_requests/12")
		if !ok {
			t.Fatal("expected parse to succeed")
		}
		if provider != "gitlab" {
			t.Fatalf("provider = %q, want gitlab", provider)
		}
		if repo != "group/subgroup/athena" {
			t.Fatalf("repo = %q, want group/subgroup/athena", repo)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		if _, _, ok := parseRepoFromPRURL("not-a-url"); ok {
			t.Fatal("expected parse to fail")
		}
	})
}

func TestResolveVCSProviderAndRepo(t *testing.T) {
	gh := newFakeVCSProvider("github")
	gl := newFakeVCSProvider("gitlab")
	plugins := []plugin.Plugin{gh, gl}

	t.Run("selects provider by URL host", func(t *testing.T) {
		provider, repo, ok := resolveVCSProviderAndRepo("athena", "https://github.com/drew-mcl/athena/pull/51", plugins)
		if !ok {
			t.Fatal("expected provider resolution to succeed")
		}
		if provider.Name() != "github" {
			t.Fatalf("provider = %q, want github", provider.Name())
		}
		if repo != "drew-mcl/athena" {
			t.Fatalf("repo = %q, want drew-mcl/athena", repo)
		}
	})

	t.Run("falls back for unknown URL", func(t *testing.T) {
		provider, repo, ok := resolveVCSProviderAndRepo("athena", "https://example.com/org/repo/pr/1", plugins)
		if !ok {
			t.Fatal("expected provider resolution to succeed")
		}
		if provider.Name() != "github" {
			t.Fatalf("provider = %q, want github fallback", provider.Name())
		}
		if repo != "athena" {
			t.Fatalf("repo = %q, want project fallback", repo)
		}
	})
}

func TestQueueSyncStopIsIdempotent(t *testing.T) {
	q := &QueueSync{stopCh: make(chan struct{})}

	q.Stop()
	q.Stop()
}
