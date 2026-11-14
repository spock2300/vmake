package build

import (
	"slices"
	"sync"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestBuildStatsRecordingAndSnapshot(t *testing.T) {
	stats := NewBuildStats()
	stats.RecordCompile("a:a", "src/b.c")
	stats.RecordCompile("a:a", "src/a.c")
	stats.RecordCompile("b:b", "src/a.c")
	stats.RecordLink("a:a")
	stats.RecordUpToDate()
	stats.RecordUpToDate()
	snapshot := stats.Snapshot()
	if snapshot.Compiled != 3 || snapshot.Linked != 1 || snapshot.UpToDate != 2 {
		t.Fatalf("snapshot counts = %+v", snapshot)
	}
	if !slices.Equal(snapshot.CompiledFiles, []string{"a:src/a.c", "a:src/b.c", "b:src/a.c"}) {
		t.Fatalf("compiled files = %v", snapshot.CompiledFiles)
	}
	if snapshot.CompiledFiles == nil {
		t.Fatal("snapshot compiled files must not be nil")
	}
}

func TestBuildStatsConcurrentRecording(t *testing.T) {
	stats := NewBuildStats()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				stats.RecordCompile("p:app", "src/main.c")
				stats.RecordUpToDate()
				stats.RecordLink("p:app")
			}
		}(i)
	}
	wg.Wait()
	snapshot := stats.Snapshot()
	if snapshot.Compiled != 400 || snapshot.Linked != 400 || snapshot.UpToDate != 400 {
		t.Fatalf("snapshot counts = %+v", snapshot)
	}
	if !slices.Equal(snapshot.CompiledFiles, []string{"p:src/main.c"}) {
		t.Fatalf("compiled files = %v", snapshot.CompiledFiles)
	}
}

func TestBuildStatsQualifiedChangedFiles(t *testing.T) {
	stats := NewBuildStats()
	stats.RecordCompile("app:app", "src/main.c")
	stats.RecordCompile("app:app", "src/main.c")
	stats.RecordCompile("lib:lib", "src/main.c")
	stats.RecordCompile("lib:lib", "/absolute/generated/table.c")
	stats.RecordCompile("lib:lib", "vendor:src/vendor.c")
	stats.RecordCompile("standalone", "src/only.c")
	snapshot := stats.Snapshot()
	want := []string{"/absolute/generated/table.c", "app:src/main.c", "lib:src/main.c", "standalone:src/only.c", "vendor:src/vendor.c"}
	if !slices.Equal(snapshot.CompiledFiles, want) {
		t.Fatalf("compiled files = %v, want %v", snapshot.CompiledFiles, want)
	}
	if snapshot.Compiled != 6 {
		t.Fatalf("compiled count = %d", snapshot.Compiled)
	}
}

func TestSchedulerRecordsBuildStats(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "main.c", "int main(void) { return 0; }\n")
	target := makeTargetWithDeps("app").SetKind(api.TargetBinary).AddFiles("main.c")
	s := nativeTestScheduler(t, dir, target)
	if s.Stats() == nil {
		t.Fatal("scheduler has no stats")
	}
	if err := s.Build("p:app"); err != nil {
		t.Fatal(err)
	}
	snapshot := s.Stats().Snapshot()
	if snapshot.Compiled != 1 || snapshot.Linked != 1 || snapshot.UpToDate != 0 {
		t.Fatalf("first build stats = %+v", snapshot)
	}
	if !slices.Equal(snapshot.CompiledFiles, []string{"p:main.c"}) {
		t.Fatalf("compiled files = %v", snapshot.CompiledFiles)
	}
	if err := s.Build("p:app"); err != nil {
		t.Fatal(err)
	}
	snapshot = s.Stats().Snapshot()
	if snapshot.Compiled != 1 || snapshot.Linked != 1 || snapshot.UpToDate != 2 {
		t.Fatalf("no-op build stats = %+v", snapshot)
	}
}
