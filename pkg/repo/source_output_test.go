package repo

import (
	"io"
	"os"
	"strings"
	"testing"
)

func captureSourceManagerStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		defer r.Close()
		var sb strings.Builder
		_, _ = io.Copy(&sb, r)
		done <- sb.String()
	}()
	defer func() {
		os.Stdout = old
		_ = w.Close()
	}()
	fn()
	_ = w.Close()
	return <-done
}

func TestEnsureSourceLogsResolutionAndFetch(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	url := fileURL(t, src)

	manager := NewSourceManager(t.TempDir(), t.TempDir())
	req := SourceRequest{Key: "native/sample", URLs: []string{url}, Version: "1.0.0", Ref: "v1.0.0"}
	out := captureSourceManagerStdout(t, func() {
		if _, err := manager.EnsureSource(req); err != nil {
			t.Fatal(err)
		}
	})

	for _, want := range []string{
		"native/sample: resolving v1.0.0 from " + url,
		"native/sample: preparing source ",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q:\n%s", want, out)
		}
	}
	resolving := strings.Index(out, "native/sample: resolving ")
	preparing := strings.Index(out, "native/sample: preparing source ")
	if resolving > preparing {
		t.Fatalf("resolving line should precede preparing line:\n%s", out)
	}

	reused := captureSourceManagerStdout(t, func() {
		if _, err := manager.EnsureSource(req); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(reused, ": resolving ") || strings.Contains(reused, ": preparing source ") {
		t.Fatalf("reusable source tree emitted network status lines:\n%s", reused)
	}
}

func TestEnsureSourceHEADLogsResolutionAndPrepare(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	url := fileURL(t, src)
	head := ShortCommit(gitOutput(t, src, "rev-parse", "HEAD"))

	out := captureSourceManagerStdout(t, func() {
		_, err := NewSourceManager(t.TempDir(), t.TempDir()).EnsureSource(SourceRequest{
			Key: "native/sample", URLs: []string{url},
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	for _, want := range []string{
		"native/sample: resolving HEAD from " + url,
		"native/sample: preparing source " + head + " from " + url,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q:\n%s", want, out)
		}
	}
}

func TestEnsureSourceCommitPinSkipsResolutionLog(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	url := fileURL(t, src)

	cache := t.TempDir()
	first, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{
		Key: "native/sample", URLs: []string{url}, Version: "1.0.0", Ref: "v1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Commit == "" {
		t.Fatal("initial materialization returned an empty commit")
	}

	out := captureSourceManagerStdout(t, func() {
		_, err := NewSourceManager(t.TempDir(), cache).EnsureSource(SourceRequest{
			Key: "native/sample", URLs: []string{url}, Version: "1.0.0", Commit: first.Commit,
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	want := "native/sample: preparing source " + ShortCommit(first.Commit) + " from " + url
	if !strings.Contains(out, want) {
		t.Fatalf("stdout missing %q:\n%s", want, out)
	}
	if strings.Contains(out, ": resolving ") {
		t.Fatalf("commit-pinned source resolved a ref remotely:\n%s", out)
	}
}
