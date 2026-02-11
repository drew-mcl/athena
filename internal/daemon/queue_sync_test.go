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

func (f *fakeVCSProvider) GetMergeReadiness(ctx context.Context, repo, branch string) (*vcs.MergeReadiness, error) {
	return &vcs.MergeReadiness{}, nil
}

func (f *fakeVCSProvider) MergePR(ctx context.Context, repo, branch string, method vcs.MergeMethod) error {
	return nil
}

func TestParseRepoFromPRURL(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		wantProvider string
		wantRepo     string
		wantOK       bool
	}{
		{
			name:         "github",
			url:          "https://github.com/drew-mcl/athena/pull/51",
			wantProvider: "github",
			wantRepo:     "drew-mcl/athena",
			wantOK:       true,
		},
		{
			name:         "github enterprise style host",
			url:          "https://github.internal.example/org/repo/pull/9",
			wantProvider: "github",
			wantRepo:     "org/repo",
			wantOK:       true,
		},
		{
			name:         "gitlab with subgroup",
			url:          "https://gitlab.com/group/subgroup/athena/-/merge_requests/12",
			wantProvider: "gitlab",
			wantRepo:     "group/subgroup/athena",
			wantOK:       true,
		},
		{
			name:   "invalid",
			url:    "not-a-url",
			wantOK: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider, repo, ok := parseRepoFromPRURL(test.url)
			if ok != test.wantOK {
				t.Fatalf("ok = %v, want %v", ok, test.wantOK)
			}
			if !ok {
				return
			}
			if provider != test.wantProvider {
				t.Fatalf("provider = %q, want %q", provider, test.wantProvider)
			}
			if repo != test.wantRepo {
				t.Fatalf("repo = %q, want %q", repo, test.wantRepo)
			}
		})
	}
}

func TestResolveVCSProviderAndRepo(t *testing.T) {
	gh := newFakeVCSProvider("github")
	gl := newFakeVCSProvider("gitlab")
	plugins := []plugin.Plugin{gh, gl}
	tests := []struct {
		name         string
		project      string
		prURL        string
		wantProvider string
		wantRepo     string
		wantOK       bool
	}{
		{
			name:         "selects provider by URL host",
			project:      "athena",
			prURL:        "https://github.com/drew-mcl/athena/pull/51",
			wantProvider: "github",
			wantRepo:     "drew-mcl/athena",
			wantOK:       true,
		},
		{
			name:         "falls back for unknown URL",
			project:      "athena",
			prURL:        "https://example.com/org/repo/pr/1",
			wantProvider: "github",
			wantRepo:     "athena",
			wantOK:       true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider, repo, ok := resolveVCSProviderAndRepo(test.project, test.prURL, plugins)
			if ok != test.wantOK {
				t.Fatalf("ok = %v, want %v", ok, test.wantOK)
			}
			if !ok {
				return
			}
			if provider == nil {
				t.Fatal("provider = nil, want non-nil")
			}
			if provider.Name() != test.wantProvider {
				t.Fatalf("provider = %q, want %q", provider.Name(), test.wantProvider)
			}
			if repo != test.wantRepo {
				t.Fatalf("repo = %q, want %q", repo, test.wantRepo)
			}
		})
	}
}

func TestQueueSyncStopIsIdempotent(t *testing.T) {
	q := &QueueSync{stopCh: make(chan struct{})}

	q.Stop()
	q.Stop()
}
