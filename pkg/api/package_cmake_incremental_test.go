package api

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/jsonio"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type cmakeIncrementalFixture struct {
	pkg    *Package
	source string
	build  string
	bin    string
}

func newCMakeIncrementalFixture(t *testing.T) *cmakeIncrementalFixture {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	build := filepath.Join(source, "build", "key")
	bin := filepath.Join(root, "bin")
	for _, dir := range []string{source, bin} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeFakeCMake(t, bin, "cmake executable one\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	p := NewPackage().SetDirs(PkgDirs{SourceDir: source, BuildDir: build}).SetToolchain(&toolchain.Toolchain{})
	if err := os.MkdirAll(p.CMakeBuildDir(), 0755); err != nil {
		t.Fatal(err)
	}
	return &cmakeIncrementalFixture{pkg: p, source: source, build: build, bin: bin}
}

func writeFakeCMake(t *testing.T, bin, body string) {
	t.Helper()
	name := "cmake"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
}

func writeCMakeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func (f *cmakeIncrementalFixture) configureArgs(t *testing.T, extra ...string) []string {
	t.Helper()
	args, err := f.pkg.cmakeConfigureArgs(runtime.GOOS, extra...)
	if err != nil {
		t.Fatal(err)
	}
	return args
}

// writeCMakeConfigureOutput creates the files a successful configure leaves in
// the build directory: the cache, CMakeFiles bookkeeping and the generator's
// top-level build file for args.
func writeCMakeConfigureOutput(t *testing.T, buildDir string, args []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(buildDir, "CMakeFiles"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "CMakeCache.txt"), []byte("cache\n"), 0644); err != nil {
		t.Fatal(err)
	}
	main := cmakeGeneratorMainFile(args)
	if main == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(buildDir, main), []byte("generated\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func symlinkOrSkip(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		if runtime.GOOS == "windows" && errors.Is(err, fs.ErrPermission) {
			t.Skipf("symlink creation requires Developer Mode or elevation: %v", err)
		}
		t.Fatal(err)
	}
}

func (f *cmakeIncrementalFixture) saveStamp(t *testing.T, extra ...string) []string {
	t.Helper()
	args := f.configureArgs(t, extra...)
	writeCMakeConfigureOutput(t, f.pkg.CMakeBuildDir(), args)
	if err := f.pkg.saveCMakeConfigureStamp(args, ""); err != nil {
		t.Fatal(err)
	}
	return args
}

func TestCMakeConfigureStampRoundTrip(t *testing.T) {
	f := newCMakeIncrementalFixture(t)
	writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
	args := f.saveStamp(t, "-G", "Ninja")
	if _, ok := f.pkg.cmakeConfigureState(args); !ok {
		t.Fatal("freshly saved configure stamp is not up to date")
	}
	if data, err := os.ReadFile(f.pkg.cmakeConfigureStampPath()); err != nil || len(data) == 0 {
		t.Fatalf("stamp not written: %q, %v", data, err)
	}
}

func TestCMakeConfigureFingerprintTracksSourceFiles(t *testing.T) {
	f := newCMakeIncrementalFixture(t)
	writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
	writeCMakeFile(t, f.source, "cmake/module.cmake", "set(MODULE 1)\n")
	args := f.saveStamp(t, "-G", "Ninja")
	if _, ok := f.pkg.cmakeConfigureState(args); !ok {
		t.Fatal("unaltered source tree is not up to date")
	}
	writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.21)\n")
	if _, ok := f.pkg.cmakeConfigureState(args); ok {
		t.Fatal("changed CMakeLists.txt did not invalidate the configure stamp")
	}
	writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
	writeCMakeFile(t, f.source, "extra.cmake", "set(EXTRA 1)\n")
	if _, ok := f.pkg.cmakeConfigureState(args); ok {
		t.Fatal("new .cmake file did not invalidate the configure stamp")
	}
	writeCMakeFile(t, f.source, "CMakePresets.json", "{\"version\": 3}\n")
	if _, ok := f.pkg.cmakeConfigureState(args); ok {
		t.Fatal("new CMakePresets.json did not invalidate the configure stamp")
	}
}

func TestCMakeConfigureFingerprintIgnoresCompiledSources(t *testing.T) {
	f := newCMakeIncrementalFixture(t)
	writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
	args := f.saveStamp(t, "-G", "Ninja")
	writeCMakeFile(t, f.source, "probe.c", "int probe(void) { return 1; }\n")
	writeCMakeFile(t, f.source, "probe.h", "#define PROBE 1\n")
	if _, ok := f.pkg.cmakeConfigureState(args); !ok {
		t.Fatal("compiled source change invalidated the configure stamp")
	}
}

func TestCMakeConfigureFingerprintExcludesBuildDirectories(t *testing.T) {
	f := newCMakeIncrementalFixture(t)
	writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
	args := f.saveStamp(t, "-G", "Ninja")
	writeCMakeFile(t, f.pkg.CMakeBuildDir(), "generated.cmake", "set(GENERATED 1)\n")
	if _, ok := f.pkg.cmakeConfigureState(args); !ok {
		t.Fatal("build directory cmake file invalidated the configure stamp")
	}
	writeCMakeFile(t, f.build, "other.cmake", "set(OTHER 1)\n")
	if _, ok := f.pkg.cmakeConfigureState(args); !ok {
		t.Fatal("package build directory cmake file invalidated the configure stamp")
	}
	writeCMakeFile(t, filepath.Join(f.source, ".git"), "hook.cmake", "set(HOOK 1)\n")
	writeCMakeFile(t, filepath.Join(f.source, ".vmake_deps", "dep"), "dep.cmake", "set(DEP 1)\n")
	writeCMakeFile(t, filepath.Join(f.source, "build", "other-key"), "keyed.cmake", "set(KEYED 1)\n")
	writeCMakeFile(t, filepath.Join(f.source, "CMakeFiles"), "generated.cmake", "set(GENERATED 1)\n")
	writeCMakeFile(t, f.source, "cmake_install.cmake", "set(INSTALL 1)\n")
	if _, ok := f.pkg.cmakeConfigureState(args); !ok {
		t.Fatal(".git, .vmake_deps, sibling build keys or generated CMake files invalidated the configure stamp")
	}
}

func TestCMakeConfigureFingerprintExcludesSiblingBuildKeys(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	bin := filepath.Join(root, "bin")
	for _, dir := range []string{source, bin} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeFakeCMake(t, bin, "cmake executable one\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	p := NewPackage().SetDirs(PkgDirs{
		SourceDir: source,
		BuildDir:  filepath.Join(source, "build", "key-a"),
	}).SetToolchain(&toolchain.Toolchain{})
	if err := os.MkdirAll(p.CMakeBuildDir(), 0755); err != nil {
		t.Fatal(err)
	}
	writeCMakeFile(t, source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
	args, err := p.cmakeConfigureArgs(runtime.GOOS, "-G", "Ninja")
	if err != nil {
		t.Fatal(err)
	}
	writeCMakeConfigureOutput(t, p.CMakeBuildDir(), args)
	if err := p.saveCMakeConfigureStamp(args, ""); err != nil {
		t.Fatal(err)
	}
	writeCMakeFile(t, filepath.Join(source, "build", "key-b"), "other.cmake", "set(OTHER 1)\n")
	if _, ok := p.cmakeConfigureState(args); !ok {
		t.Fatal("a sibling build key directory invalidated the configure stamp")
	}
}

func TestCMakeConfigureFingerprintFollowsSymlinkedSrcDir(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "package")
	source := filepath.Join(root, "actual source")
	build := filepath.Join(project, "build", "key")
	bin := filepath.Join(root, "bin")
	for _, dir := range []string{project, source, bin} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeFakeCMake(t, bin, "cmake executable one\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	link := filepath.Join(project, "src")
	symlinkOrSkip(t, source, link)
	writeCMakeFile(t, source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
	p := NewPackage().SetDirs(PkgDirs{SourceDir: project, BuildDir: build}).SetToolchain(&toolchain.Toolchain{})
	p.SetSrcDir(link)
	if err := os.MkdirAll(p.CMakeBuildDir(), 0755); err != nil {
		t.Fatal(err)
	}
	args, err := p.cmakeConfigureArgs(runtime.GOOS, "-G", "Ninja")
	if err != nil {
		t.Fatal(err)
	}
	writeCMakeConfigureOutput(t, p.CMakeBuildDir(), args)
	if err := p.saveCMakeConfigureStamp(args, ""); err != nil {
		t.Fatal(err)
	}
	files, err := p.cmakeConfigureInputFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("symlinked SrcDir produced no configure input files")
	}
	if _, ok := p.cmakeConfigureState(args); !ok {
		t.Fatal("unaltered symlinked source tree is not up to date")
	}
	writeCMakeFile(t, source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.21)\n")
	if _, ok := p.cmakeConfigureState(args); ok {
		t.Fatal("CMakeLists change through a symlinked SrcDir did not invalidate the stamp")
	}
}

func TestCMakeConfigureFingerprintSkipsUnreadableInputFiles(t *testing.T) {
	f := newCMakeIncrementalFixture(t)
	writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
	broken := filepath.Join(f.source, "broken.cmake")
	symlinkOrSkip(t, filepath.Join(f.source, "missing-target"), broken)
	files, err := f.pkg.cmakeConfigureInputFiles()
	if err != nil {
		t.Fatalf("unreadable input file returned an error: %v", err)
	}
	for _, file := range files {
		if file.Rel == "broken.cmake" {
			t.Fatal("unreadable input file was included in the fingerprint")
		}
	}
	args := f.saveStamp(t, "-G", "Ninja")
	if _, ok := f.pkg.cmakeConfigureState(args); !ok {
		t.Fatal("unreadable input file invalidated the configure stamp")
	}
}

func TestCMakeConfigureFingerprintTracksArguments(t *testing.T) {
	f := newCMakeIncrementalFixture(t)
	writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
	f.saveStamp(t, "-G", "Ninja")
	for _, extra := range [][]string{
		{"-G", "Unix Makefiles"},
		{"-G", "Ninja", "-DEXTRA=1"},
		{"-G", "Ninja", "-DCMAKE_C_FLAGS=-DPROJECT"},
	} {
		if _, ok := f.pkg.cmakeConfigureState(f.configureArgs(t, extra...)); ok {
			t.Fatalf("argument change %q did not invalidate the configure stamp", extra)
		}
	}
	f.pkg.SetCMakeInstallDir("stage")
	if _, ok := f.pkg.cmakeConfigureState(f.configureArgs(t, "-G", "Ninja")); ok {
		t.Fatal("install prefix change did not invalidate the configure stamp")
	}
	f.pkg.SetGlobalFlags([]string{"-DGLOBAL_C"}, nil, nil, nil)
	if _, ok := f.pkg.cmakeConfigureState(f.configureArgs(t, "-G", "Ninja")); ok {
		t.Fatal("global flag change did not invalidate the configure stamp")
	}
}

func TestCMakeConfigureFingerprintTracksCMakeExecutable(t *testing.T) {
	f := newCMakeIncrementalFixture(t)
	writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
	args := f.saveStamp(t, "-G", "Ninja")
	if _, ok := f.pkg.cmakeConfigureState(args); !ok {
		t.Fatal("saved configure stamp is not up to date")
	}
	writeFakeCMake(t, f.bin, "cmake executable two\n")
	if _, ok := f.pkg.cmakeConfigureState(args); ok {
		t.Fatal("replaced cmake executable did not invalidate the configure stamp")
	}
}

func TestCMakeConfigureFingerprintUsesToolchainPath(t *testing.T) {
	f := newCMakeIncrementalFixture(t)
	writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
	install := filepath.Join(t.TempDir(), "toolchain space")
	installBin := filepath.Join(install, "bin")
	if err := os.MkdirAll(installBin, 0755); err != nil {
		t.Fatal(err)
	}
	writeFakeCMake(t, installBin, "toolchain cmake one\n")
	f.pkg.SetToolchain(&toolchain.Toolchain{InstallPath: install})
	args := f.saveStamp(t, "-G", "Ninja")
	if _, ok := f.pkg.cmakeConfigureState(args); !ok {
		t.Fatal("saved configure stamp is not up to date")
	}
	// The toolchain bin directory precedes the inherited PATH, so the process-PATH
	// cmake is not the executed binary and must not affect the identity.
	writeFakeCMake(t, f.bin, "process cmake changed\n")
	if _, ok := f.pkg.cmakeConfigureState(args); !ok {
		t.Fatal("process PATH cmake change invalidated a toolchain-resolved identity")
	}
	writeFakeCMake(t, installBin, "toolchain cmake two\n")
	if _, ok := f.pkg.cmakeConfigureState(args); ok {
		t.Fatal("toolchain cmake change did not invalidate the configure stamp")
	}
}

func TestCMakeConfigureFingerprintTracksEnvironment(t *testing.T) {
	f := newCMakeIncrementalFixture(t)
	writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
	args := f.saveStamp(t, "-G", "Ninja")
	t.Setenv("CMAKE_GENERATOR", "Ninja")
	if _, ok := f.pkg.cmakeConfigureState(args); ok {
		t.Fatal("CMAKE_GENERATOR change did not invalidate the configure stamp")
	}
	t.Setenv("CMAKE_GENERATOR", "")
	t.Setenv("CMAKE_PREFIX_PATH", filepath.Join(f.bin, "prefix"))
	if _, ok := f.pkg.cmakeConfigureState(args); ok {
		t.Fatal("CMAKE_PREFIX_PATH change did not invalidate the configure stamp")
	}
	t.Setenv("CMAKE_PREFIX_PATH", "")
	t.Setenv("PATH", f.bin+string(os.PathListSeparator)+"unused"+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, ok := f.pkg.cmakeConfigureState(args); ok {
		t.Fatal("PATH change did not invalidate the configure stamp")
	}
}

func TestCMakeConfigureStampInvalidation(t *testing.T) {
	t.Run("missing stamp", func(t *testing.T) {
		f := newCMakeIncrementalFixture(t)
		writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
		args := f.saveStamp(t, "-G", "Ninja")
		if err := os.Remove(f.pkg.cmakeConfigureStampPath()); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.pkg.cmakeConfigureState(args); ok {
			t.Fatal("missing stamp reported up to date")
		}
	})
	t.Run("garbage stamp", func(t *testing.T) {
		f := newCMakeIncrementalFixture(t)
		writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
		args := f.saveStamp(t, "-G", "Ninja")
		if err := os.WriteFile(f.pkg.cmakeConfigureStampPath(), []byte("not json"), 0644); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.pkg.cmakeConfigureState(args); ok {
			t.Fatal("garbage stamp reported up to date")
		}
	})
	t.Run("version mismatch", func(t *testing.T) {
		f := newCMakeIncrementalFixture(t)
		writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
		args := f.saveStamp(t, "-G", "Ninja")
		if err := os.WriteFile(f.pkg.cmakeConfigureStampPath(), []byte(`{"version":999,"fingerprint":"stale"}`), 0644); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.pkg.cmakeConfigureState(args); ok {
			t.Fatal("stamp with an unknown version reported up to date")
		}
	})
	t.Run("missing cache", func(t *testing.T) {
		f := newCMakeIncrementalFixture(t)
		writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
		args := f.saveStamp(t, "-G", "Ninja")
		if _, ok := f.pkg.cmakeConfigureState(args); !ok {
			t.Fatal("saved configure stamp is not up to date")
		}
		if err := os.Remove(filepath.Join(f.pkg.CMakeBuildDir(), "CMakeCache.txt")); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.pkg.cmakeConfigureState(args); ok {
			t.Fatal("missing CMakeCache.txt reported up to date")
		}
	})
	t.Run("dry run", func(t *testing.T) {
		f := newCMakeIncrementalFixture(t)
		writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
		args := f.saveStamp(t, "-G", "Ninja")
		if err := os.Remove(f.pkg.cmakeConfigureStampPath()); err != nil {
			t.Fatal(err)
		}
		f.pkg.SetDryRun(true)
		if _, ok := f.pkg.cmakeConfigureState(args); ok {
			t.Fatal("dry-run reported configure up to date")
		}
		f.pkg.CMakeConfigure("-G", "Ninja")
		if _, err := os.Stat(f.pkg.cmakeConfigureStampPath()); !os.IsNotExist(err) {
			t.Fatalf("dry-run wrote a configure stamp: %v", err)
		}
	})
}

// TestCMakeConfigureOutputMarkers checks that reuse also requires the
// generator's configure output: a hand-deleted build.ninja or CMakeFiles
// directory must force configure instead of failing the later build.
func TestCMakeConfigureOutputMarkers(t *testing.T) {
	t.Run("missing generator file", func(t *testing.T) {
		f := newCMakeIncrementalFixture(t)
		writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
		args := f.saveStamp(t, "-G", "Ninja")
		if _, ok := f.pkg.cmakeConfigureState(args); !ok {
			t.Fatal("saved configure stamp is not up to date")
		}
		if err := os.Remove(filepath.Join(f.pkg.CMakeBuildDir(), "build.ninja")); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.pkg.cmakeConfigureState(args); ok {
			t.Fatal("missing build.ninja reported up to date")
		}
	})
	t.Run("missing CMakeFiles", func(t *testing.T) {
		f := newCMakeIncrementalFixture(t)
		writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
		args := f.saveStamp(t, "-G", "Ninja")
		if err := os.RemoveAll(filepath.Join(f.pkg.CMakeBuildDir(), "CMakeFiles")); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.pkg.cmakeConfigureState(args); ok {
			t.Fatal("missing CMakeFiles reported up to date")
		}
	})
	t.Run("unknown generator needs CMakeFiles only", func(t *testing.T) {
		f := newCMakeIncrementalFixture(t)
		writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
		args := f.configureArgs(t, "--preset", "release")
		writeCMakeConfigureOutput(t, f.pkg.CMakeBuildDir(), args)
		if err := f.pkg.saveCMakeConfigureStamp(args, ""); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.pkg.cmakeConfigureState(args); !ok {
			t.Fatal("preset configure stamp is not up to date")
		}
		if err := os.RemoveAll(filepath.Join(f.pkg.CMakeBuildDir(), "CMakeFiles")); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.pkg.cmakeConfigureState(args); ok {
			t.Fatal("preset configure without CMakeFiles reported up to date")
		}
	})
}

func TestCMakeGeneratorMainFile(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"ninja flag", []string{"-G", "Ninja"}, "build.ninja"},
		{"joined ninja flag", []string{"-GNinja"}, "build.ninja"},
		{"makefiles flag", []string{"-G", "Unix Makefiles"}, "Makefile"},
		{"multi config ninja", []string{"-G", "Ninja Multi-Config"}, "build.ninja"},
		{"preset generator unknown", []string{"--preset", "release"}, ""},
		{"foreign generator unknown", []string{"-G", "Visual Studio 17 2022"}, ""},
	}
	for _, tc := range cases {
		if got := cmakeGeneratorMainFile(tc.args); got != tc.want {
			t.Errorf("cmakeGeneratorMainFile(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}
	t.Setenv("CMAKE_GENERATOR", "Ninja")
	if got := cmakeGeneratorMainFile(nil); got != "build.ninja" {
		t.Errorf("CMAKE_GENERATOR Ninja -> %q, want build.ninja", got)
	}
	t.Setenv("CMAKE_GENERATOR", "")
	want := "Makefile"
	if runtime.GOOS == "windows" {
		want = "build.ninja"
	}
	if got := cmakeGeneratorMainFile(nil); got != want {
		t.Errorf("host default -> %q, want %q", got, want)
	}
}

// TestCMakePathCandidatesMatchesExecLookup pins the candidate generation to
// internal/exec's command lookup: with PATHEXT in effect an extensionless bare
// name is never tried, while a name that already carries an extension keeps
// its bare form.
func TestCMakePathCandidatesMatchesExecLookup(t *testing.T) {
	suffixes := pathextSuffixes(".COM;.EXE")
	if got := lookPathCandidates("cmake", suffixes); !reflect.DeepEqual(got, []string{"cmake.COM", "cmake.EXE"}) {
		t.Fatalf("candidates for an extensionless name = %q", got)
	}
	if got := lookPathCandidates("cmake.exe", suffixes); !reflect.DeepEqual(got, []string{"cmake.exe", "cmake.exe.COM", "cmake.exe.EXE"}) {
		t.Fatalf("candidates for an extension name = %q", got)
	}
	if got := lookPathCandidates("cmake", nil); !reflect.DeepEqual(got, []string{"cmake"}) {
		t.Fatalf("candidates without PATHEXT = %q", got)
	}
	if got := pathextSuffixes("EXE;.CMD"); !reflect.DeepEqual(got, []string{".EXE", ".CMD"}) {
		t.Fatalf("pathextSuffixes without leading dots = %q", got)
	}
	if got := pathextSuffixes(""); !reflect.DeepEqual(got, []string{".COM", ".EXE", ".BAT", ".CMD"}) {
		t.Fatalf("pathextSuffixes default = %q", got)
	}
}

// TestCMakeCommandEnvironmentMergesToolchain checks cmake resolution reads the
// synthetic environment: the process environment overlaid with the toolchain's
// PATH, which is what the runner passes to cmake.
func TestCMakeCommandEnvironmentMergesToolchain(t *testing.T) {
	t.Setenv("VMAKE_TEST_SENTINEL", "process")
	p := NewPackage().SetToolchain(&toolchain.Toolchain{})
	if got := p.cmakeCommandEnvironment()["VMAKE_TEST_SENTINEL"]; got != "process" {
		t.Fatalf("process environment missing from the synthetic env: %q", got)
	}
	install := filepath.Join(t.TempDir(), "toolchain")
	if err := os.MkdirAll(filepath.Join(install, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	p.SetToolchain(&toolchain.Toolchain{InstallPath: install})
	if got := p.cmakeCommandEnvironment()["PATH"]; !strings.HasPrefix(got, filepath.Join(install, "bin")) {
		t.Fatalf("toolchain PATH did not override the process PATH: %q", got)
	}
}

// TestCMakeConfigureFingerprintScansUpstreamBuildDirForRemoteLayout pins the
// exclusion root to the invocation's build root: a remote package's SourceDir
// is its upstream checkout, so an upstream build/ directory is a configure
// input even though vmake never writes one there.
func TestCMakeConfigureFingerprintScansUpstreamBuildDirForRemoteLayout(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "src", "sample")
	output := filepath.Join(root, "out", "owner")
	bin := filepath.Join(root, "bin")
	for _, dir := range []string{source, filepath.Join(output, "key"), bin} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeFakeCMake(t, bin, "cmake executable one\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	p := NewPackage().SetDirs(PkgDirs{
		SourceDir:  source,
		BuildDir:   filepath.Join(output, "key", "build"),
		InstallDir: filepath.Join(output, "key", "install"),
	}).SetToolchain(&toolchain.Toolchain{})
	if err := os.MkdirAll(p.CMakeBuildDir(), 0755); err != nil {
		t.Fatal(err)
	}
	writeCMakeFile(t, source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
	args, err := p.cmakeConfigureArgs(runtime.GOOS, "-G", "Ninja")
	if err != nil {
		t.Fatal(err)
	}
	writeCMakeConfigureOutput(t, p.CMakeBuildDir(), args)
	if err := p.saveCMakeConfigureStamp(args, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.cmakeConfigureState(args); !ok {
		t.Fatal("saved configure stamp is not up to date")
	}
	writeCMakeFile(t, filepath.Join(source, "build"), "upstream.cmake", "set(UPSTREAM 1)\n")
	if _, ok := p.cmakeConfigureState(args); ok {
		t.Fatal("an upstream build/ directory was excluded from the fingerprint")
	}
}

// TestCMakeConfigureStateReusesFingerprint checks a required reconfigure gets
// the fingerprint computed during the up-to-date check, so a fingerprint miss
// does not hash the tree twice.
func TestCMakeConfigureStateReusesFingerprint(t *testing.T) {
	f := newCMakeIncrementalFixture(t)
	writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
	args := f.saveStamp(t, "-G", "Ninja")
	writeCMakeFile(t, f.source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.21)\n")
	fingerprint, ok := f.pkg.cmakeConfigureState(args)
	if ok || fingerprint == "" {
		t.Fatalf("fingerprint miss did not return the current fingerprint: %q, %v", fingerprint, ok)
	}
	if err := f.pkg.saveCMakeConfigureStamp(args, fingerprint); err != nil {
		t.Fatal(err)
	}
	var stamp cmakeConfigureStamp
	if err := jsonio.Load(f.pkg.cmakeConfigureStampPath(), &stamp); err != nil {
		t.Fatal(err)
	}
	want, err := f.pkg.cmakeConfigureFingerprint(args)
	if err != nil {
		t.Fatal(err)
	}
	if stamp.Fingerprint != want {
		t.Fatalf("reused fingerprint %q != recomputed %q", stamp.Fingerprint, want)
	}
}
