package repo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func newFaultGitTransport(failResolveRemote, failShallowCommit bool, cloneFullCalls *int) *gitTransport {
	base := defaultGitTransport()
	t := base
	if failResolveRemote {
		t.ResolveRemoteRef = func(context.Context, []string, string) (string, string, error) {
			return "", "", errors.New("simulated network failure")
		}
		t.ResolveRemoteRefOne = func(context.Context, string, string) (string, string, error) {
			return "", "", errors.New("simulated network failure")
		}
	}
	if failShallowCommit {
		t.FetchShallowCommit = func(context.Context, string, string, string) error {
			return errors.New("server does not allow request for unadvertised object")
		}
	}
	if cloneFullCalls != nil {
		t.CloneFull = func(ctx context.Context, url, dir string) error {
			*cloneFullCalls++
			return base.CloneFull(ctx, url, dir)
		}
	}
	return &t
}

func TestSourceManagerTransportSeamFallsBackToCachedRefAndFullClone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	origin := t.TempDir()
	runGitForTest := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", origin}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGitForTest("init", "-q", "-b", "master")
	runGitForTest("config", "user.email", "test@example.com")
	runGitForTest("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(origin, "lib.c"), []byte("int x = 1;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitForTest("add", "-A")
	runGitForTest("commit", "-q", "-m", "v1")
	runGitForTest("tag", "v1.0.0")
	commit, err := GetCurrentCommit(origin)
	if err != nil {
		t.Fatal(err)
	}
	url := RepoFileURL(origin)

	cacheDir := t.TempDir()
	mgr1 := NewSourceManager(t.TempDir(), cacheDir)
	res1, err := mgr1.EnsureSource(SourceRequest{
		Key:     "official/foo",
		URLs:    []string{url},
		Version: "1.0.0",
		Ref:     "v1.0.0",
	})
	if err != nil || res1.Commit != commit {
		t.Fatalf("initial EnsureSource: res=%+v, err=%v", res1, err)
	}

	mgr2 := NewSourceManager(t.TempDir(), cacheDir)
	mgr2.transport = newFaultGitTransport(true, false, nil)
	res2, err := mgr2.EnsureSource(SourceRequest{
		Key:     "official/foo",
		URLs:    []string{url},
		Version: "1.0.0",
		Ref:     "v1.0.0",
	})
	if err != nil || res2.Commit != commit {
		t.Fatalf("fallback EnsureSource from ObjectCache: res=%+v, err=%v", res2, err)
	}

	mgr3 := NewSourceManager(t.TempDir(), t.TempDir())
	mgr3.cache.enabled = false
	cloneFullCalls := 0
	mgr3.transport = newFaultGitTransport(false, true, &cloneFullCalls)
	res3, err := mgr3.EnsureSource(SourceRequest{
		Key:    "official/foo",
		URLs:   []string{url},
		Commit: commit,
	})
	if err != nil || res3.Commit != commit || cloneFullCalls != 1 {
		t.Fatalf("CloneFull fallback: res=%+v, err=%v, cloneFullCalls=%d", res3, err, cloneFullCalls)
	}
}
