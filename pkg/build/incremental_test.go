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

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func nativeTestScheduler(t *testing.T, dir string, targets ...*api.Target) *Scheduler {
	t.Helper()
	return nativeTestSchedulerWithSession(t, dir, nil, targets...)
}

func nativeTestSchedulerWithSession(t *testing.T, dir string, session *Session, targets ...*api.Target) *Scheduler {
	t.Helper()
	tools := toolchain.Tools{}
	for name, destination := range map[string]*string{"gcc": &tools.CC, "g++": &tools.CXX, "ar": &tools.AR} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Skipf("%s unavailable", name)
		}
		*destination = path
	}
	graph, err := NewBuildGraph(makeTargets("p", targets...), nil)
	if err != nil {
		t.Fatal(err)
	}
	tc := &toolchain.Toolchain{Name: "test", Tools: tools}
	platform := api.Platform{OS: runtime.GOOS}
	resolvedTools, err := ResolveTools(tc, platform)
	if err != nil {
		t.Fatal(err)
	}
	dirs := api.PkgDirs{SourceDir: dir, BuildDir: filepath.Join(dir, "build")}
	pkg := api.NewPackage().SetName("p").SetDirs(dirs).SetSrcDir(dir)
	pkg.SetToolchain(tc)
	pkg.SetPlatform(platform)
	return newScheduler(schedulerConfig{
		Graph:     graph,
		Toolchain: tc,
		Platform:  platform,
		Layouts:   map[string]*PackageLayout{"p": {Dirs: dirs, BuildKey: "k", Ready: true}},
		Packages:  map[string]*api.Package{"p": pkg},
		Mode:      api.ModeDebug,
		Tools:     resolvedTools,
		RootDir:   dir,
		Session:   session,
	})
}

func readNativeValue(t *testing.T, scheduler *Scheduler, target string) string {
	t.Helper()
	output := PlacementFor(scheduler.graph.Nodes["p:"+target].Target, scheduler.pkgs[scheduler.graph.Nodes["p:"+target].PkgName], scheduler.targetOS(scheduler.graph.Nodes["p:"+target].PkgName)).OutputPath
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
	s.numWorkers = 4
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
	s.numWorkers = 4
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
	archive := PlacementFor(s.graph.Nodes["p:library"].Target, s.pkgs[s.graph.Nodes["p:library"].PkgName], s.targetOS(s.graph.Nodes["p:library"].PkgName)).OutputPath
	out, err := exec.Command(s.resolvedTools.AR, "t", archive).CombinedOutput()
	if err != nil {
		t.Fatalf("archive members: %s: %v", out, err)
	}
	var members []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		// macOS ar adds a __.SYMDEF index member that is not a source object.
		if line == "" || strings.HasPrefix(line, "__.SYMDEF") {
			continue
		}
		members = append(members, line)
	}
	if len(members) != 1 {
		t.Fatalf("removed source retained archive member: %s", out)
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
	linkFlag := "-Wl,-Map=" + mapFile
	if runtime.GOOS == "darwin" {
		linkFlag = "-Wl,-dead_strip"
	}
	app.AddLdFlags(linkFlag)
	if err := s.Build("p:app"); err != nil {
		t.Fatal(err)
	}
	if links != 2 {
		t.Fatalf("link flags changed but linked %d times", links)
	}
	if runtime.GOOS != "darwin" {
		if _, err := os.Stat(mapFile); err != nil {
			t.Fatal(err)
		}
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
	buildFresh := func() *Scheduler {
		t.Helper()
		platform := api.Platform{OS: runtime.GOOS}
		tools, err := ResolveTools(&tc, platform)
		if err != nil {
			t.Fatal(err)
		}
		key, err := BuildKeyE(tools.CCKey(), api.ModeDebug, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		fresh := newScheduler(schedulerConfig{
			Graph:     s.graph,
			Toolchain: &tc,
			Platform:  platform,
			Layouts:   map[string]*PackageLayout{"p": {Dirs: api.PkgDirs{SourceDir: dir, BuildDir: filepath.Join(dir, "build")}, BuildKey: key, Ready: true}},
			Mode:      api.ModeDebug,
			Tools:     tools,
		})
		if err := fresh.Build("p:app"); err != nil {
			t.Fatal(err)
		}
		return fresh
	}
	first := buildFresh()
	if got := readNativeValue(t, first, "app"); got != "1" {
		t.Fatalf("first tool produced %s", got)
	}
	writeTool("2")
	second := buildFresh()
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
	output := PlacementFor(s.graph.Nodes["p:app"].Target, s.pkgs[s.graph.Nodes["p:app"].PkgName], s.targetOS(s.graph.Nodes["p:app"].PkgName)).OutputPath
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "original:changed" {
		t.Fatalf("post-link retry reused partial output: %q: %v", data, err)
	}
	info, err := os.Lstat(output)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("prebuilt output is not an independent regular file: %v", err)
	}
}

func TestPrebuiltPublication(t *testing.T) {
	target := makeTargetWithDeps("prebuilt").SetKind(api.TargetStatic).SetPrebuilt("build/staging/original.a")
	s, _ := newTestScheduler(t, makeTargets("p", target))
	s.hashes = newFileHashCache()
	s.stats = NewBuildStats()
	s.platform = api.Platform{OS: "none", Triple: "arm-none-eabi"}
	pkg := s.pkgs["p"]
	src := pkg.ResolvePath(target.Prebuilt())
	writeAssemblyFixture(t, pkg.SourceDir, target.Prebuilt(), "first")
	output := PlacementFor(target, pkg, s.targetOS("p")).OutputPath
	build := func() {
		t.Helper()
		if err := s.Build("p:prebuilt"); err != nil {
			t.Fatal(err)
		}
	}
	check := func(want string) {
		t.Helper()
		data, err := os.ReadFile(output)
		if err != nil || string(data) != want {
			t.Fatalf("published prebuilt = %q, want %q: %v", data, want, err)
		}
	}

	build()
	check("first")
	first, err := os.Lstat(output)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if !first.Mode().IsRegular() {
			t.Fatalf("Windows prebuilt output mode = %v, want a regular file", first.Mode())
		}
	} else if first.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("Unix prebuilt output mode = %v, want a symlink", first.Mode())
	}
	build()
	again, err := os.Lstat(output)
	if err != nil || !os.SameFile(first, again) || !first.ModTime().Equal(again.ModTime()) || s.stats.Snapshot().Linked != 1 {
		t.Fatalf("unchanged build republished %s: %v", output, err)
	}

	// CMake staging can produce different bytes with identical size and mtime.
	original, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	writeAssemblyFixture(t, pkg.SourceDir, target.Prebuilt(), "other")
	if err := os.Chtimes(src, original.ModTime(), original.ModTime()); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		check("first") // The published copy is independent.
	}
	build()
	check("other")
	if runtime.GOOS == "windows" {
		if err := os.Remove(src); err != nil {
			t.Fatal(err)
		}
		if err := s.Build("p:prebuilt"); err == nil {
			t.Fatal("missing source was accepted")
		}
		check("other")
		writeAssemblyFixture(t, pkg.SourceDir, target.Prebuilt(), "retry")
		build()
		check("retry")
	}
}

func TestPrebuiltCopyReplacesOldSymlink(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "new.a", "new")
	writeAssemblyFixture(t, dir, "old.a", "old")
	dst := filepath.Join(dir, "output.a")
	if err := os.Symlink(filepath.Join(dir, "old.a"), dst); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("cannot create old privileged symlink for migration fixture: %v", err)
		}
		t.Fatal(err)
	}
	cmd := commandSpec{Program: "copy", Args: []string{filepath.Join(dir, "new.a"), dst}}
	if err := realizePrebuilt(dir, cmd); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(dst)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("migration did not publish a regular file: %v", err)
	}
	for path, want := range map[string]string{dst: "new", filepath.Join(dir, "old.a"): "old"} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("%s = %q, want %s: %v", path, data, want, err)
		}
	}
}

func TestPrebuiltCopyPublishFailureCleansStaging(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "source.a", "new")
	writeAssemblyFixture(t, dir, "output.a/existing", "previous")
	cmd := commandSpec{Program: "copy", Args: []string{filepath.Join(dir, "source.a"), filepath.Join(dir, "output.a")}}
	if err := realizePrebuilt(dir, cmd); err == nil {
		t.Fatal("replacing a nonempty directory succeeded")
	}
	data, err := os.ReadFile(filepath.Join(dir, "output.a", "existing"))
	if err != nil || string(data) != "previous" {
		t.Fatalf("destination after failed replacement = %q: %v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("staging file left behind: %v: %v", entries, err)
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
	output := PlacementFor(s.graph.Nodes["p:library"].Target, s.pkgs[s.graph.Nodes["p:library"].PkgName], s.targetOS(s.graph.Nodes["p:library"].PkgName)).OutputPath
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
	s.numWorkers = 2
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
	if _, _, _, err := s.compileSourceChanged(ctx, &ResolvedTarget{Node: s.graph.Nodes["p:app"]}, "slow.c"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled compile = %v", err)
	}
}

func TestBuildProductPath(t *testing.T) {
	root := t.TempDir()
	info := &PkgInfo{PkgDirs: api.PkgDirs{
		SourceDir:  root,
		BuildDir:   filepath.Join(root, "build", "key"),
		InstallDir: filepath.Join(root, "out", "install"),
	}}
	for _, path := range []string{
		filepath.Join(root, "build", "key", "staging", "lib", "libx.a"),
		filepath.Join(root, "out", "install", "lib", "libx.a"),
	} {
		if !buildProductPath(info, root, path) {
			t.Errorf("buildProductPath(%q) = false, want true", path)
		}
	}
	if buildProductPath(info, root, filepath.Join(root, "vendor", "libx.a")) {
		t.Errorf("buildProductPath reported a user file as a build product")
	}
	if buildProductPath(nil, root, filepath.Join(root, "out", "install", "lib", "libx.a")) {
		t.Errorf("buildProductPath accepted a nil package")
	}
}
