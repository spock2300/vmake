package build

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/spock2300/vmake/pkg/api"
)

func TestBuildStatsRecordingAndSnapshot(t *testing.T) {
	stats := NewBuildStats()
	if stats.ChangedTarget("a:a") {
		t.Fatal("fresh stats report a changed target")
	}
	stats.RecordCompile("a:a", "src/b.c")
	stats.RecordCompile("a:a", "src/a.c")
	stats.RecordCompile("b:b", "src/a.c")
	stats.RecordLink("a:a")
	stats.RecordUpToDate()
	stats.RecordUpToDate()
	stats.RecordTargetDuration("a:a", 25*time.Millisecond)
	snapshot := stats.Snapshot()
	if snapshot.Compiled != 3 || snapshot.Linked != 1 || snapshot.UpToDate != 2 {
		t.Fatalf("snapshot counts = %+v", snapshot)
	}
	if !slices.Equal(snapshot.CompiledFiles, []string{"src/a.c", "src/b.c"}) {
		t.Fatalf("compiled files = %v", snapshot.CompiledFiles)
	}
	if !slices.Equal(snapshot.ChangedTargets, []string{"a:a", "b:b"}) {
		t.Fatalf("changed targets = %v", snapshot.ChangedTargets)
	}
	if !stats.ChangedTarget("a:a") || !stats.ChangedTarget("b:b") || stats.ChangedTarget("c:c") {
		t.Fatal("changed target lookup is wrong")
	}
	if got := stats.TargetDuration("a:a"); got != 25 {
		t.Fatalf("target duration = %d", got)
	}
	if got := stats.TargetDuration("b:b"); got != 0 {
		t.Fatalf("unrecorded target duration = %d", got)
	}
	if snapshot.CompiledFiles == nil || snapshot.ChangedTargets == nil {
		t.Fatal("snapshot slices must not be nil")
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
	if !slices.Equal(snapshot.CompiledFiles, []string{"src/main.c"}) {
		t.Fatalf("compiled files = %v", snapshot.CompiledFiles)
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
	if !s.Stats().ChangedTarget("p:app") {
		t.Fatal("built target is not marked changed")
	}
	if !slices.Equal(snapshot.CompiledFiles, []string{"main.c"}) {
		t.Fatalf("compiled files = %v", snapshot.CompiledFiles)
	}
	if s.TargetDuration("p:app") < 0 {
		t.Fatalf("target duration = %d", s.TargetDuration("p:app"))
	}
	if err := s.Build("p:app"); err != nil {
		t.Fatal(err)
	}
	snapshot = s.Stats().Snapshot()
	if snapshot.Compiled != 1 || snapshot.Linked != 1 || snapshot.UpToDate != 2 {
		t.Fatalf("no-op build stats = %+v", snapshot)
	}
}
