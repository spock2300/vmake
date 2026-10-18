package build

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func snapshotCheckTree(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = fmt.Sprintf("%d|%d|%x", info.Size(), info.ModTime().UnixNano(), sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestSchedulerCheckPlansWithoutExecuting(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "main.c", "int main(void) { return 0; }\n")
	writeAssemblyFixture(t, dir, "blob.bin", "blob bytes")
	callbackCalls := 0
	app := makeTargetWithDeps("app").SetKind(api.TargetBinary).AddFiles("main.c").AddBinHeader("blob.bin")
	gen := makeTargetWithDeps("gen").SetKind(api.TargetVoid).SetBuildFunc(func(*api.Package) error {
		callbackCalls++
		return os.WriteFile(filepath.Join(dir, "callback.txt"), []byte("ran"), 0644)
	})
	s := nativeTestScheduler(t, dir, app, gen)
	buildDir := filepath.Join(dir, "build")
	generatedHeader := filepath.Join(buildDir, "generated", "blob.h")

	s.check = true
	s.stats = NewBuildStats()
	if err := s.BuildAll(); err != nil {
		t.Fatal(err)
	}
	snapshot := s.Stats().Snapshot()
	if snapshot.PlannedCompiles != 1 || snapshot.PlannedLinks != 1 || snapshot.PlannedCallbacks != 1 {
		t.Fatalf("planned work = %+v, want 1 compile, 1 link, 1 callback", snapshot)
	}
	if snapshot.Compiled != 0 || snapshot.Linked != 0 || snapshot.UpToDate != 0 {
		t.Fatalf("check executed actions: %+v", snapshot)
	}
	if callbackCalls != 0 {
		t.Fatalf("check executed the void callback %d time(s)", callbackCalls)
	}
	for _, path := range []string{buildDir, generatedHeader, filepath.Join(dir, "callback.txt")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("check created %s: %v", path, err)
		}
	}

	s.check = false
	s.stats = NewBuildStats()
	if err := s.BuildAll(); err != nil {
		t.Fatal(err)
	}
	if callbackCalls != 1 {
		t.Fatalf("real build ran the callback %d time(s), want 1", callbackCalls)
	}
	if _, err := os.Stat(generatedHeader); err != nil {
		t.Fatalf("real build did not generate the header: %v", err)
	}

	before := snapshotCheckTree(t, buildDir)
	s.check = true
	s.stats = NewBuildStats()
	if err := s.BuildAll(); err != nil {
		t.Fatal(err)
	}
	snapshot = s.Stats().Snapshot()
	if snapshot.PlannedCompiles != 0 || snapshot.PlannedLinks != 0 || snapshot.UpToDate != 2 || snapshot.PlannedCallbacks != 1 {
		t.Fatalf("up-to-date check = %+v, want 2 up-to-date actions and 1 planned callback", snapshot)
	}
	if callbackCalls != 1 {
		t.Fatalf("up-to-date check ran the callback: %d calls", callbackCalls)
	}
	if after := snapshotCheckTree(t, buildDir); !maps.Equal(before, after) {
		t.Fatalf("check mutated the build directory:\nbefore: %v\nafter:  %v", before, after)
	}

	writeAssemblyFixture(t, dir, "main.c", "int main(void) { return 1; }\n")
	s.stats = NewBuildStats()
	if err := s.BuildAll(); err != nil {
		t.Fatal(err)
	}
	snapshot = s.Stats().Snapshot()
	if snapshot.PlannedCompiles != 1 || snapshot.PlannedLinks != 1 || snapshot.UpToDate != 0 {
		t.Fatalf("source edit check = %+v, want 1 compile and 1 link planned", snapshot)
	}
	if after := snapshotCheckTree(t, buildDir); !maps.Equal(before, after) {
		t.Fatalf("pending check mutated the build directory:\nbefore: %v\nafter:  %v", before, after)
	}
}
