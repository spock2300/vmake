package build

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func nativeTestScheduler(t *testing.T, dir string, targets ...*api.Target) *Scheduler {
	t.Helper()
	tools := toolchain.Tools{}
	for name, destination := range map[string]*string{"gcc": &tools.CC, "g++": &tools.CXX, "ar": &tools.AR} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Skipf("%s unavailable", name)
		}
		*destination = path
	}
	graph, err := NewBuildGraph(makeTargets("p", targets...), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewScheduler(graph, &toolchain.Toolchain{Name: "test", Tools: tools}, map[string]*api.PkgDirs{"p": {SourceDir: dir, BuildDir: filepath.Join(dir, "build")}}, api.ModeDebug, nil, api.Platform{OS: runtime.GOOS})
	if err != nil {
		t.Fatal(err)
	}
	s.SetRootDir(dir)
	return s
}

func readNativeValue(t *testing.T, scheduler *Scheduler, target string) string {
	t.Helper()
	output := scheduler.getTargetOutputPath(scheduler.graph.Nodes["p:"+target])
	data, err := exec.Command(output).CombinedOutput()
	if err != nil {
		t.Fatalf("run %s: %s: %v", output, data, err)
	}
	return strings.TrimSpace(string(data))
}

func TestNativeObjectsSeparateTargetsAndSourcePaths(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "main.c", "#include <stdio.h>\nint main(void) { printf(\"%d\\n\", VALUE); return 0; }\n")
	one := makeTargetWithDeps("one").SetKind(api.TargetBinary).AddFiles("main.c").AddDefines("VALUE=1")
	two := makeTargetWithDeps("two").SetKind(api.TargetBinary).AddFiles("main.c").AddDefines("VALUE=2")
	s := nativeTestScheduler(t, dir, one, two)
	if err := s.BuildAll(); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"one": "1", "two": "2"} {
		if got := readNativeValue(t, s, name); got != want {
			t.Fatalf("%s = %s, want %s", name, got, want)
		}
	}
	writeAssemblyFixture(t, dir, "src/value.c", "int nested(void) { return 3; }\n")
	writeAssemblyFixture(t, dir, "src_value.c", "int flat(void) { return 4; }\n")
	writeAssemblyFixture(t, dir, "paths.c", "#include <stdio.h>\nint nested(void); int flat(void); int main(void) { printf(\"%d\\n\", nested()+flat()); return 0; }\n")
	paths := makeTargetWithDeps("paths").SetKind(api.TargetBinary).AddFiles("paths.c", "src/value.c", "src_value.c", "src/*.c")
	s = nativeTestScheduler(t, dir, paths)
	s.SetNumWorkers(4)
	if err := s.BuildAll(); err != nil {
		t.Fatal(err)
	}
	if got := readNativeValue(t, s, "paths"); got != "7" {
		t.Fatalf("colliding source names = %s, want 7", got)
	}
	if len(s.ccWriter.commands) != 3 {
		t.Fatalf("overlapping globs compiled %d sources, want 3", len(s.ccWriter.commands))
	}
}

func TestNativeSameStemDifferentLanguages(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("ELF assembly fixture")
	}
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "value.c", "int c_value(void) { return 1; }\n")
	writeAssemblyFixture(t, dir, "value.cpp", "extern \"C\" int cpp_value(void) { return 2; }\n")
	writeAssemblyFixture(t, dir, "value.S", ".data\n.globl asm_value\nasm_value:\n.long 3\n.section .note.GNU-stack,\"\",@progbits\n")
	writeAssemblyFixture(t, dir, "main.c", "#include <stdio.h>\nint c_value(void); int cpp_value(void); extern int asm_value;\nint main(void) { printf(\"%d\\n\", c_value() + cpp_value() + asm_value); return 0; }\n")
	library := makeTargetWithDeps("values").SetKind(api.TargetStatic).AddFiles("value.c", "value.cpp", "value.S")
	target := makeTargetWithDeps("app", "values").SetKind(api.TargetBinary).AddFiles("main.c")
	s := nativeTestScheduler(t, dir, library, target)
	s.SetNumWorkers(4)
	if err := s.BuildAll(); err != nil {
		t.Fatal(err)
	}
	if got := readNativeValue(t, s, "app"); got != "6" {
		t.Fatalf("mixed language result = %s, want 6", got)
	}
	before := make(map[string]time.Time)
	for _, command := range s.ccWriter.commands {
		if strings.TrimSuffix(filepath.Base(command.File), filepath.Ext(command.File)) != "value" {
			continue
		}
		for index, arg := range command.Arguments {
			if arg != "-o" || index+1 == len(command.Arguments) {
				continue
			}
			output := command.Arguments[index+1]
			if filepath.Base(output) != "value.o" {
				t.Fatalf("object lost source name: %s", output)
			}
			info, err := os.Stat(output)
			if err != nil {
				t.Fatal(err)
			}
			before[output] = info.ModTime()
		}
	}
	if len(before) != 3 {
		t.Fatalf("same-stem sources produced %d distinct objects", len(before))
	}
	if err := s.BuildAll(); err != nil {
		t.Fatal(err)
	}
	for output, modified := range before {
		info, err := os.Stat(output)
		if err != nil || !info.ModTime().Equal(modified) {
			t.Fatalf("unchanged object rebuilt: %s: %v", output, err)
		}
	}
}

func TestNativeCompileSignatureTracksOrderEnvironmentAndContent(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "main.c", "#include <stdio.h>\n#include <value.h>\nint main(void) { printf(\"%d\\n\", VALUE + HEADER); return 0; }\n")
	for name, value := range map[string]string{"a": "10", "b": "20"} {
		writeAssemblyFixture(t, dir, name+"/value.h", "#define HEADER "+value+"\n")
	}
	t.Setenv("CPATH", filepath.Join(dir, "a"))
	makeTarget := func(flags ...string) *api.Target {
		return makeTargetWithDeps("app").SetKind(api.TargetBinary).AddFiles("main.c").AddCFlags(flags)
	}
	target := makeTarget("-DVALUE=1", "-UVALUE", "-DVALUE=2", "-DVALUE=1")
	s := nativeTestScheduler(t, dir, target)
	var compiles atomic.Int32
	s.compiler.run = func(name, dir string, args ...string) ([]byte, error) {
		compiles.Add(1)
		return gnuRunner(s.compiler.env)(name, dir, args...)
	}
	build := func(want string) {
		t.Helper()
		if err := s.Build("p:app"); err != nil {
			t.Fatal(err)
		}
		if got := readNativeValue(t, s, "app"); got != want {
			t.Fatalf("value = %s, want %s", got, want)
		}
	}
	build("11")
	build("11")
	if compiles.Load() != 1 {
		t.Fatalf("unchanged target compiled %d times", compiles.Load())
	}
	s.graph.Nodes["p:app"].Target = makeTarget("-DVALUE=1", "-DVALUE=1", "-UVALUE", "-DVALUE=2")
	build("12")
	t.Setenv("CPATH", filepath.Join(dir, "b"))
	build("22")
	header := filepath.Join(dir, "b", "value.h")
	info, err := os.Stat(header)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(header, []byte("#define HEADER 30\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(header, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	build("32")
	if compiles.Load() != 4 {
		t.Fatalf("changed inputs compiled %d times, want 4", compiles.Load())
	}
}

func TestNativeLinkSignatureTracksSourceRemovalAndFlags(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "a.c", "int a(void) { return 1; }\n")
	writeAssemblyFixture(t, dir, "b.c", "int b(void) { return 2; }\n")
	target := makeTargetWithDeps("library").SetKind(api.TargetStatic).AddFiles("a.c", "b.c")
	s := nativeTestScheduler(t, dir, target)
	if err := s.Build("p:library"); err != nil {
		t.Fatal(err)
	}
	s.graph.Nodes["p:library"].Target = makeTargetWithDeps("library").SetKind(api.TargetStatic).AddFiles("a.c")
	if err := s.Build("p:library"); err != nil {
		t.Fatal(err)
	}
	archive := s.getTargetOutputPath(s.graph.Nodes["p:library"])
	members, err := exec.Command(s.resolvedTools.AR, "t", archive).CombinedOutput()
	if err != nil {
		t.Fatalf("archive members: %s: %v", members, err)
	}
	if len(strings.Fields(string(members))) != 1 {
		t.Fatalf("removed source retained archive member: %s", members)
	}
	writeAssemblyFixture(t, dir, "main.c", "int main(void) { return 0; }\n")
	app := makeTargetWithDeps("app").SetKind(api.TargetBinary).AddFiles("main.c")
	s = nativeTestScheduler(t, dir, app)
	var links int
	s.linker.run = func(name, dir string, args ...string) ([]byte, error) {
		links++
		return gnuRunner(s.resolvedTools.env)(name, dir, args...)
	}
	if err := s.Build("p:app"); err != nil {
		t.Fatal(err)
	}
	mapFile := filepath.Join(dir, "link.map")
	app.AddLdFlags("-Wl,-Map=" + mapFile)
	if err := s.Build("p:app"); err != nil {
		t.Fatal(err)
	}
	if links != 2 {
		t.Fatalf("link flags changed but linked %d times", links)
	}
	if _, err := os.Stat(mapFile); err != nil {
		t.Fatal(err)
	}
}

func TestNativeToolContentChangeWithStableVersionRebuilds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell compiler wrapper")
	}
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "main.c", "#include <stdio.h>\nint main(void) { printf(\"%d\\n\", VALUE); return 0; }\n")
	target := makeTargetWithDeps("app").SetKind(api.TargetBinary).AddFiles("main.c")
	s := nativeTestScheduler(t, dir, target)
	tool := filepath.Join(dir, "compiler.sh")
	cc := s.resolvedTools.CC
	writeTool := func(value string) {
		t.Helper()
		if err := os.WriteFile(tool, []byte("#!/bin/sh\nexec '"+strings.ReplaceAll(cc, "'", "'\\''")+"' -DVALUE="+value+" \"$@\"\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeTool("1")
	tc := *s.toolchain
	tc.Tools.CC = tool
	newScheduler := func() *Scheduler {
		t.Helper()
		fresh, err := NewScheduler(s.graph, &tc, map[string]*api.PkgDirs{"p": {SourceDir: dir, BuildDir: filepath.Join(dir, "build")}}, api.ModeDebug, nil, api.Platform{OS: runtime.GOOS})
		if err != nil {
			t.Fatal(err)
		}
		if err := fresh.Build("p:app"); err != nil {
			t.Fatal(err)
		}
		return fresh
	}
	first := newScheduler()
	if got := readNativeValue(t, first, "app"); got != "1" {
		t.Fatalf("first tool produced %s", got)
	}
	writeTool("2")
	second := newScheduler()
	if first.resolvedTools.CCVersion != second.resolvedTools.CCVersion {
		t.Fatal("fixture changed version output")
	}
	if first.resolvedTools.CCKey() == second.resolvedTools.CCKey() {
		t.Fatal("changed compiler bytes retained tool identity")
	}
	if got := readNativeValue(t, second, "app"); got != "2" {
		t.Fatalf("changed tool produced stale value %s", got)
	}
}

func TestPrebuiltPostLinkFailurePreservesInputAndRetries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "original", "original")
	writeAssemblyFixture(t, dir, "post.sh", "#!/bin/sh\nprintf ':changed' >> \"$1\"\nif [ ! -f once ]; then touch once; exit 1; fi\n")
	tool := filepath.Join(dir, "post.sh")
	if err := os.Chmod(tool, 0755); err != nil {
		t.Fatal(err)
	}
	target := makeTargetWithDeps("app").SetKind(api.TargetBinary).SetPrebuilt("original").AddPostLink("objcopy", "{output}")
	s := nativeTestScheduler(t, dir, target)
	s.resolvedTools.OBJCOPY = tool
	if err := s.Build("p:app"); err == nil {
		t.Fatal("post-link failure was ignored")
	}
	if err := s.Build("p:app"); err != nil {
		t.Fatal(err)
	}
	if err := s.Build("p:app"); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(dir, "original"))
	if err != nil || string(original) != "original" {
		t.Fatalf("prebuilt source mutated: %q: %v", original, err)
	}
	output := s.getTargetOutputPath(s.graph.Nodes["p:app"])
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "original:changed" {
		t.Fatalf("post-link retry reused partial output: %q: %v", data, err)
	}
	info, err := os.Lstat(output)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("prebuilt output is not an independent regular file: %v", err)
	}
}

func TestPrebuiltWithoutPostLinkRetainsSymlink(t *testing.T) {
	if !fs.SymlinksSupported() {
		t.Skip("symlinks unavailable")
	}
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "original", "original")
	target := makeTargetWithDeps("prebuilt").SetKind(api.TargetStatic).SetPrebuilt("original")
	s := nativeTestScheduler(t, dir, target)
	if err := s.Build("p:prebuilt"); err != nil {
		t.Fatal(err)
	}
	output := s.getTargetOutputPath(s.graph.Nodes["p:prebuilt"])
	info, err := os.Lstat(output)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("prebuilt output is not a symlink: %v", err)
	}
	if err := s.Build("p:prebuilt"); err != nil {
		t.Fatal(err)
	}
}

func TestPublishedHeadersRefreshWhenLibraryIsUnchanged(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "library.c", "int library(void) { return 1; }\n")
	writeAssemblyFixture(t, dir, "include/public.h", "#define PUBLIC 1\n")
	target := makeTargetWithDeps("library").SetKind(api.TargetStatic).AddFiles("library.c").AddPublicIncludes("include")
	s := nativeTestScheduler(t, dir, target)
	s.pkgs["p"].InstallDir = filepath.Join(dir, "install")
	if err := s.Build("p:library"); err != nil {
		t.Fatal(err)
	}
	output := s.getTargetOutputPath(s.graph.Nodes["p:library"])
	before, err := FileHash(output)
	if err != nil {
		t.Fatal(err)
	}
	writeAssemblyFixture(t, dir, "include/public.h", "#define PUBLIC 2\n")
	installed := filepath.Join(dir, "install", "include", "public.h")
	for _, remove := range []bool{false, true} {
		if remove {
			if err := os.Remove(installed); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.Build("p:library"); err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(installed)
		if err != nil || string(content) != "#define PUBLIC 2\n" {
			t.Fatalf("published header is stale or missing: %q: %v", content, err)
		}
	}
	after, err := FileHash(output)
	if err != nil || before != after {
		t.Fatalf("library changed while testing header publication: %v", err)
	}
}

func TestCompileFailureCancelsWorkersAndRemovesPartialOutputs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell compiler fixture")
	}
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "slow.c", "slow\n")
	writeAssemblyFixture(t, dir, "fail.c", "fail\n")
	writeAssemblyFixture(t, dir, "compiler.sh", "#!/bin/sh\nobj=''\nwhile [ $# -gt 0 ]; do\n case \"$1\" in -o) shift; obj=$1;; *.c) src=$1;; esac\n shift\ndone\nif [ \"$src\" = fail.c ]; then\n while [ ! -f started ]; do sleep 0.01; done\n exit 1\nfi\nprintf partial > \"$obj\"\nprintf '%s: %s\\n' \"$obj\" \"$src\" > \"$obj.d\"\ntouch started\nsleep 30\ntouch finished\n")
	tool := filepath.Join(dir, "compiler.sh")
	if err := os.Chmod(tool, 0755); err != nil {
		t.Fatal(err)
	}
	target := makeTargetWithDeps("app").SetKind(api.TargetBinary).AddFiles("slow.c", "fail.c")
	s := nativeTestScheduler(t, dir, target)
	s.compiler = NewCompiler(&ResolvedTools{CC: tool, CXX: tool})
	s.SetNumWorkers(2)
	start := time.Now()
	if err := s.Build("p:app"); err == nil || !strings.Contains(err.Error(), "fail.c") {
		t.Fatalf("original compiler failure was not preserved: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("workers were not cancelled: %v", elapsed)
	}
	for _, name := range []string{"slow.c", "fail.c"} {
		rel, err := objectPath("p:app", name, dir)
		if err != nil {
			t.Fatal(err)
		}
		obj := s.pkgs["p"].OutputPath(rel)
		for _, path := range []string{obj, obj + ".d", obj + ".vmake.json"} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("failed compile left output %s: %v", path, err)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "finished")); !os.IsNotExist(err) {
		t.Fatal("cancelled compiler completed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := s.compileSourceContext(ctx, &ResolvedTarget{Node: s.graph.Nodes["p:app"]}, "slow.c"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled compile = %v", err)
	}
}
