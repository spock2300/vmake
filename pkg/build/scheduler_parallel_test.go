package build

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spock2300/vmake/pkg/api"
)

type parallelRecorder struct {
	mu       sync.Mutex
	start    map[string]time.Time
	end      map[string]time.Time
	running  atomic.Int32
	maxConc  atomic.Int32
	failPkgs map[string]bool
	delay    time.Duration
}

func newParallelRecorder(failPkgs ...string) *parallelRecorder {
	fail := map[string]bool{}
	for _, p := range failPkgs {
		fail[p] = true
	}
	return &parallelRecorder{
		start:    map[string]time.Time{},
		end:      map[string]time.Time{},
		failPkgs: fail,
		delay:    30 * time.Millisecond,
	}
}

func (r *parallelRecorder) hook(s *Scheduler) func([]string) error {
	return func(fullNames []string) error {
		pkgName := packageNameOfFullName(fullNames[0])
		cur := r.running.Add(1)
		for {
			m := r.maxConc.Load()
			if cur <= m || r.maxConc.CompareAndSwap(m, cur) {
				break
			}
		}
		r.mu.Lock()
		r.start[pkgName] = time.Now()
		r.mu.Unlock()

		time.Sleep(r.delay)

		r.running.Add(-1)
		r.mu.Lock()
		r.end[pkgName] = time.Now()
		r.mu.Unlock()
		if r.failPkgs[pkgName] {
			return errors.New("boom: " + pkgName)
		}
		return nil
	}
}

func packageNameOfFullName(fn string) string {
	for i := len(fn) - 1; i >= 0; i-- {
		if fn[i] == ':' {
			return fn[:i]
		}
	}
	return fn
}

func newParallelTestScheduler(t *testing.T, targets map[string]map[string]*api.Target) (*Scheduler, *BuildGraph) {
	t.Helper()
	graph, err := NewBuildGraph(targets, nil, nil)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	root := t.TempDir()
	s := &Scheduler{
		graph:    graph,
		pkgs:     map[string]*PkgInfo{},
		packages: map[string]*api.Package{},
		ccWriter: NewCompileCommandsWriter(&ResolvedTools{CC: "cc", CXX: "c++"}),
		rootDir:  root,
	}
	for pkgName := range targets {
		dir := filepath.Join(root, pkgName)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		s.SetPkgDirs(pkgName, &api.PkgDirs{SourceDir: dir, BuildDir: filepath.Join(dir, "build")})
	}
	return s, graph
}

func multiPkgTargets(deps map[string][]string) map[string]map[string]*api.Target {
	out := map[string]map[string]*api.Target{}
	for pkg, ds := range deps {
		tr := api.NewTargetRegistry()
		tgt := tr.Target(pkg).SetKind(api.TargetVoid)
		for _, d := range ds {
			tgt.AddDeps(d + ":" + d)
		}
		out[pkg] = map[string]*api.Target{pkg: tgt}
	}
	return out
}

func TestBuildAllParallelSchedulesConcurrently(t *testing.T) {
	targets := multiPkgTargets(map[string][]string{
		"a":  nil,
		"b":  nil,
		"ab": {"a", "b"},
	})
	s, _ := newParallelTestScheduler(t, targets)
	rec := newParallelRecorder()
	s.buildTargetsFn = rec.hook(s)
	s.SetParallelPkgs(4)

	if err := s.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if rec.maxConc.Load() < 2 {
		t.Errorf("expected concurrent package builds, max concurrency = %d", rec.maxConc.Load())
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if !rec.start["ab"].After(rec.end["a"]) || !rec.start["ab"].After(rec.end["b"]) {
		t.Error("dependent package ab must start only after deps a and b finish")
	}
}

func TestBuildAllParallelFailureCascade(t *testing.T) {
	targets := multiPkgTargets(map[string][]string{
		"a": nil,
		"b": nil,
		"c": {"a"},
		"d": {"c"},
		"e": {"b"},
	})
	s, _ := newParallelTestScheduler(t, targets)
	rec := newParallelRecorder("a")
	s.buildTargetsFn = rec.hook(s)
	s.SetKeepGoing(true)
	s.SetParallelPkgs(4)

	err := s.BuildAll()
	if err == nil {
		t.Fatal("BuildAll should report the failed package")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, built := range []string{"b", "e"} {
		if _, ok := rec.start[built]; !ok {
			t.Errorf("independent package %s should still build with keep-going", built)
		}
	}
	for _, skipped := range []string{"c", "d"} {
		if _, ok := rec.start[skipped]; ok {
			t.Errorf("package %s should be skipped after dependency failure", skipped)
		}
	}
}

func TestBuildAllParallelStopsWithoutKeepGoing(t *testing.T) {
	targets := multiPkgTargets(map[string][]string{
		"a": nil,
		"b": nil,
	})
	s, _ := newParallelTestScheduler(t, targets)
	rec := newParallelRecorder("a")
	rec.delay = 0
	s.buildTargetsFn = rec.hook(s)
	s.SetParallelPkgs(2)

	done := make(chan error, 1)
	go func() { done <- s.BuildAll() }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("BuildAll should fail")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("BuildAll hung: worker termination broken")
	}
}

func TestPkgInfoOfMissingPkg(t *testing.T) {
	s := &Scheduler{}
	if _, err := s.pkgInfoOf("ghost"); err == nil {
		t.Error("pkgInfoOf should error for unregistered package")
	}
}

func TestBuildAllParallelVoidPackagesMaterializeBeforeWorkers(t *testing.T) {
	targets := multiPkgTargets(map[string][]string{
		"a":  nil,
		"b":  nil,
		"ab": {"a", "b"},
	})
	var ran sync.Map
	for pkg, tgts := range targets {
		tgts[pkg].SetBuildFunc(func(p *api.Package) error {
			ran.Store(pkg, true)
			return nil
		})
	}
	s, _ := newParallelTestScheduler(t, targets)
	s.SetParallelPkgs(3)

	if err := s.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	for pkg := range targets {
		if _, ok := ran.Load(pkg); !ok {
			t.Errorf("void BuildFunc for %s never ran", pkg)
		}
	}
}
