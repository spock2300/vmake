package build

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestBuildJobResolvesDistinctPlatformGroups(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell tool probe")
	}
	dir := t.TempDir()
	probeLog := filepath.Join(dir, "probe.log")
	probe := filepath.Join(dir, "probe.sh")
	quoted := strings.ReplaceAll(probeLog, "'", "'\\''")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> '" + quoted + "'\nprintf 'fake version\\n'\n"
	if err := os.WriteFile(probe, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	tc := &toolchain.Toolchain{Name: "probe", Tools: toolchain.Tools{CC: probe, CXX: probe, AR: probe}}
	dirs := map[string]*api.PkgDirs{
		"p": {SourceDir: filepath.Join(dir, "p"), BuildDir: filepath.Join(dir, "p", "build")},
		"q": {SourceDir: filepath.Join(dir, "q"), BuildDir: filepath.Join(dir, "q", "build")},
	}
	packages := map[string]*api.Package{
		"p": api.NewPackage().SetPlatform(api.Platform{OS: "linux"}).SetDirs(*dirs["p"]),
		"q": api.NewPackage().SetPlatform(api.Platform{OS: "windows"}).SetDirs(*dirs["q"]),
	}
	targets := map[string]map[string]*api.Target{
		"p": {"p": makeTargetWithDeps("p").SetKind(api.TargetVoid)},
		"q": {"q": makeTargetWithDeps("q").SetKind(api.TargetVoid)},
	}
	graph, err := NewBuildGraph(targets, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	env := testBuildEnv(dirs, packages, map[string]*toolchain.Toolchain{"p": tc, "q": tc}, NewSession(nil), dir)
	job := testBuildJob(graph, tc, env, api.ModeDebug, api.Platform{OS: "none"}, false)
	if _, err := job.Run(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(probeLog)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "--version"); got != 6 {
		t.Fatalf("tool probes = %d, want 6 (base plus two platform groups):\n%s", got, data)
	}
}
