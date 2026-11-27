package repo

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOfflineSubmoduleMaterialization(t *testing.T) {
	requireGit(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); http.NotFound(w, r) }))
	defer server.Close()
	child := writePatchableSource(t)
	parent := writePatchableSource(t)
	runGit(t, parent, "-c", "protocol.file.allow=always", "submodule", "add", "-q", RepoFileURL(child), "dep")
	runGit(t, parent, "config", "-f", ".gitmodules", "submodule.dep.url", server.URL+"/dep.git")
	runGit(t, parent, "commit", "-qam", "add dep")
	mgr := NewSourceManager(t.TempDir(), t.TempDir())
	req := SourceRequest{Key: "existing", URLs: []string{RepoFileURL(parent)}, Commit: gitOutput(t, parent, "rev-parse", "HEAD")}
	initial, err := mgr.EnsureSource(req)
	if err != nil {
		t.Fatal(err)
	}
	req.Offline = true
	req.Submodules = true
	for _, key := range []string{"existing", "fresh"} {
		req.Key = key
		if _, err := mgr.EnsureSource(req); err == nil || !strings.Contains(err.Error(), "dep") {
			t.Fatalf("expected missing offline submodule dep, got %v", err)
		}
	}
	if n := requests.Load(); n != 0 {
		t.Fatalf("offline submodule contacted its upstream %d times", n)
	}
	runGit(t, initial.SrcDir, "-c", "protocol.file.allow=always", "-c", "submodule.dep.url="+RepoFileURL(child), "submodule", "update", "--init", "dep")
	if err := os.RemoveAll(filepath.Join(initial.SrcDir, "dep")); err != nil {
		t.Fatal(err)
	}
	req.Key = "existing"
	if !mgr.WouldMaterialize(req) {
		t.Fatal("missing submodule should report materialization needed")
	}
	if _, err := mgr.EnsureSource(req); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(initial.SrcDir, "dep", "lib.c")); err != nil {
		t.Fatal(err)
	}
	if mgr.WouldMaterialize(req) {
		t.Fatal("healthy submodule tree should be reusable without materialization")
	}
	for _, offline := range []bool{true, false} {
		if err := os.RemoveAll(filepath.Join(initial.SrcDir, "dep")); err != nil {
			t.Fatal(err)
		}
		req.Offline = offline
		if !mgr.WouldMaterialize(req) {
			t.Fatalf("offline=%v: deleted submodule working tree reported reusable", offline)
		}
		if _, err := mgr.EnsureSource(req); err != nil {
			t.Fatalf("offline=%v: %v", offline, err)
		}
		if _, err := os.Stat(filepath.Join(initial.SrcDir, "dep", "lib.c")); err != nil {
			t.Fatalf("offline=%v: %v", offline, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("submodule checkout from local object store contacted upstream")
	}
}
