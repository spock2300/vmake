package api

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spock2300/vmake/internal/buildruntime"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestCMakeDirectoriesAndConfiguration(t *testing.T) {
	root := filepath.Join(t.TempDir(), "工程 space")
	for _, remote := range []bool{false, true} {
		name := "local"
		if remote {
			name = "remote"
		}
		t.Run(name, func(t *testing.T) {
			dirs := PkgDirs{SourceDir: filepath.Join(root, "source"), BuildDir: filepath.Join(root, "build")}
			wantInstall := filepath.Join(dirs.BuildDir, "staging")
			if remote {
				dirs.InstallDir = filepath.Join(root, "install")
				wantInstall = dirs.InstallDir
			}
			p := NewPackage().SetDirs(dirs).SetToolchain(&toolchain.Toolchain{})
			if got := p.CMakeBuildDir(); got != filepath.Join(dirs.BuildDir, "cmake") {
				t.Fatalf("build directory = %q", got)
			}
			if got := p.CMakeInstallDir(); got != wantInstall {
				t.Fatalf("install directory = %q, want %q", got, wantInstall)
			}
			for _, mode := range []string{ModeDebug, ModeRelease, ModeSize} {
				p.CfgVals = map[string]any{ModeOptionName: mode}
				config := "Release"
				switch mode {
				case ModeDebug:
					config = "Debug"
				case ModeSize:
					config = "MinSizeRel"
				}
				assertCMakePhases(t, p, config)
			}
			if p.SetCMakeBuildDir("objects").SetCMakeInstallDir("stage").SetCMakeBuildType("MinSizeRel") != p {
				t.Fatal("CMake setters did not return the package")
			}
			if p.CMakeBuildDir() != filepath.Join(dirs.BuildDir, "objects") || p.CMakeInstallDir() != filepath.Join(dirs.BuildDir, "stage") {
				t.Fatal("relative overrides are not rooted in BuildDir")
			}
			assertCMakePhases(t, p, "MinSizeRel")
			p.SetCMakeBuildDir(filepath.Join(root, "absolute objects")).SetCMakeInstallDir(filepath.Join(root, "absolute stage"))
			if p.CMakeBuildDir() != filepath.Join(root, "absolute objects") || p.CMakeInstallDir() != filepath.Join(root, "absolute stage") {
				t.Fatal("absolute overrides changed")
			}
			assertCMakePhases(t, p, "MinSizeRel")
			if p.dirs != dirs {
				t.Fatalf("CMake changed package directories: %+v", p.dirs)
			}
		})
	}
}

func assertCMakePhases(t *testing.T, p *Package, config string) {
	t.Helper()
	configure, err := p.cmakeConfigureArgs(runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{
		filepath.ToSlash(p.CMakeBuildDir()),
		"-DCMAKE_INSTALL_PREFIX=" + filepath.ToSlash(p.CMakeInstallDir()),
		"-DCMAKE_BUILD_TYPE=" + config,
	} {
		if !slices.Contains(configure, arg) {
			t.Errorf("configure missing %q: %q", arg, configure)
		}
	}
	for _, phase := range []struct {
		name string
		args func(...string) ([]string, error)
	}{{"--build", p.cmakeBuildArgs}, {"--install", p.cmakeInstallArgs}} {
		args, err := phase.args()
		if err != nil {
			t.Fatal(err)
		}
		want := []string{phase.name, filepath.ToSlash(p.CMakeBuildDir())}
		if phase.name == "--install" {
			want = append(want, "--prefix", filepath.ToSlash(p.CMakeInstallDir()))
		}
		want = append(want, "--config", config)
		if len(args) < len(want) || !slices.Equal(args[:len(want)], want) {
			t.Errorf("%s = %q, want prefix %q", phase.name, args, want)
		}
	}
}

func TestCMakeRejectsConflictingManagedArguments(t *testing.T) {
	p := NewPackage().SetToolchain(&toolchain.Toolchain{})
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"-B", "other"}, "SetCMakeBuildDir"},
		{[]string{"-Bother"}, "SetCMakeBuildDir"},
		{[]string{"--build", "other"}, "SetCMakeBuildDir"},
		{[]string{"--install=other"}, "SetCMakeBuildDir"},
		{[]string{"--prefix", "other"}, "SetCMakeInstallDir"},
		{[]string{"--prefix=other"}, "SetCMakeInstallDir"},
		{[]string{"--install-prefix", "other"}, "SetCMakeInstallDir"},
		{[]string{"-DCMAKE_INSTALL_PREFIX=other"}, "SetCMakeInstallDir"},
		{[]string{"-DCMAKE_INSTALL_PREFIX:PATH=other"}, "SetCMakeInstallDir"},
		{[]string{"-D", "CMAKE_INSTALL_PREFIX:PATH=other"}, "SetCMakeInstallDir"},
		{[]string{"-DCMAKE_BUILD_TYPE=Release"}, "SetCMakeBuildType"},
		{[]string{"-D", "CMAKE_BUILD_TYPE:STRING=Release"}, "SetCMakeBuildType"},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			for _, phase := range []func(...string) ([]string, error){
				func(args ...string) ([]string, error) { return p.cmakeConfigureArgs(runtime.GOOS, args...) },
				p.cmakeBuildArgs,
				p.cmakeInstallArgs,
			} {
				_, err := phase(test.args...)
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Errorf("args %q: error = %v, want %s", test.args, err, test.want)
				}
			}
		})
	}
	backend := []string{"--", "-B", "--prefix=backend-value", "-DCMAKE_BUILD_TYPE=backend-value"}
	args, err := p.cmakeBuildArgs(backend...)
	if err != nil || !slices.Equal(args[len(args)-len(backend):], backend) {
		t.Fatalf("backend arguments changed: %q, %v", args, err)
	}
}

func TestCMakeConfigureLanguageAwareCompilerInjection(t *testing.T) {
	toolchainStub := func(t *testing.T) *toolchain.Toolchain {
		t.Helper()
		program, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		tool := filepath.ToSlash(program)
		return &toolchain.Toolchain{Tools: toolchain.Tools{
			CC: tool, CXX: tool, AR: tool, RANLIB: tool, STRIP: tool, OBJCOPY: tool, NM: tool,
		}}
	}
	allCompilers := []string{
		"CMAKE_C_COMPILER", "CMAKE_CXX_COMPILER", "CMAKE_ASM_COMPILER",
		"CMAKE_C_COMPILER_TARGET", "CMAKE_CXX_COMPILER_TARGET", "CMAKE_ASM_COMPILER_TARGET",
	}
	tests := []struct {
		name       string
		cmakelists string
		want       []string
		absent     []string
	}{
		{
			name:       "c only",
			cmakelists: "cmake_minimum_required(VERSION 3.20)\nproject(conly LANGUAGES C)\n",
			want:       []string{"CMAKE_C_COMPILER", "CMAKE_C_COMPILER_TARGET", "CMAKE_AR", "CMAKE_RANLIB", "CMAKE_STRIP", "CMAKE_OBJCOPY", "CMAKE_NM"},
			absent:     []string{"CMAKE_CXX_COMPILER", "CMAKE_ASM_COMPILER", "CMAKE_CXX_COMPILER_TARGET", "CMAKE_ASM_COMPILER_TARGET"},
		},
		{
			name:       "legacy c only",
			cmakelists: "cmake_minimum_required(VERSION 3.20)\nproject(conly C)\n",
			want:       []string{"CMAKE_C_COMPILER", "CMAKE_C_COMPILER_TARGET", "CMAKE_AR", "CMAKE_RANLIB", "CMAKE_STRIP", "CMAKE_OBJCOPY", "CMAKE_NM"},
			absent:     []string{"CMAKE_CXX_COMPILER", "CMAKE_ASM_COMPILER", "CMAKE_CXX_COMPILER_TARGET", "CMAKE_ASM_COMPILER_TARGET"},
		},
		{
			name:       "c and cxx",
			cmakelists: "project(app LANGUAGES C CXX)\n",
			want:       []string{"CMAKE_C_COMPILER", "CMAKE_CXX_COMPILER", "CMAKE_CXX_COMPILER_TARGET"},
			absent:     []string{"CMAKE_ASM_COMPILER", "CMAKE_ASM_COMPILER_TARGET"},
		},
		{
			name:       "asm without cxx",
			cmakelists: "project(app LANGUAGES C ASM)\n",
			want:       []string{"CMAKE_C_COMPILER", "CMAKE_ASM_COMPILER", "CMAKE_ASM_COMPILER_TARGET"},
			absent:     []string{"CMAKE_CXX_COMPILER", "CMAKE_CXX_COMPILER_TARGET"},
		},
		{
			name:       "multiline project",
			cmakelists: "cmake_minimum_required(VERSION 3.20)\nproject(\n  app\n  VERSION 1.0\n  LANGUAGES C CXX\n  DESCRIPTION \"multi line\"\n)\n",
			want:       []string{"CMAKE_CXX_COMPILER", "CMAKE_CXX_COMPILER_TARGET"},
			absent:     []string{"CMAKE_ASM_COMPILER", "CMAKE_ASM_COMPILER_TARGET"},
		},
		{
			name:       "commented project is ignored",
			cmakelists: "# project(commented LANGUAGES C)\nproject(real LANGUAGES CXX) # trailing comment\n",
			want:       []string{"CMAKE_CXX_COMPILER", "CMAKE_CXX_COMPILER_TARGET"},
			absent:     []string{"CMAKE_ASM_COMPILER", "CMAKE_ASM_COMPILER_TARGET"},
		},
		{
			name:       "project without languages",
			cmakelists: "project(app VERSION 1.0)\n",
			want:       allCompilers,
		},
		{
			name:       "missing cmakelists",
			cmakelists: "",
			want:       allCompilers,
		},
		{
			name:       "unparseable languages",
			cmakelists: "project(app LANGUAGES ${APP_LANGUAGES})\n",
			want:       allCompilers,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := t.TempDir()
			if test.cmakelists != "" {
				if err := os.WriteFile(filepath.Join(source, "CMakeLists.txt"), []byte(test.cmakelists), 0644); err != nil {
					t.Fatal(err)
				}
			}
			p := NewPackage().
				SetDirs(PkgDirs{SourceDir: source, BuildDir: t.TempDir()}).
				SetPlatform(Platform{OS: "linux", Triple: "arm-none-eabi"}).
				SetToolchain(toolchainStub(t))
			args, err := p.cmakeConfigureArgs(runtime.GOOS)
			if err != nil {
				t.Fatal(err)
			}
			definitions := cmakeDefinitions(args)
			for _, key := range test.want {
				if !definitions[key] {
					t.Errorf("missing %s in configure arguments: %q", key, args)
				}
			}
			for _, key := range test.absent {
				if definitions[key] {
					t.Errorf("unexpected %s in configure arguments: %q", key, args)
				}
			}
		})
	}
}

func TestCMakeConfigureCOnlyOmitsUnusedCompilerWarning(t *testing.T) {
	for _, program := range []string{"cmake", "ninja"} {
		if _, err := exec.LookPath(program); err != nil {
			t.Skip(err)
		}
	}
	tc := toolchain.GetBuiltinHost()
	for _, tool := range []string{tc.Tools.CC, tc.Tools.CXX, tc.Tools.AR, tc.Tools.RANLIB, tc.Tools.STRIP, tc.Tools.OBJCOPY, tc.Tools.NM} {
		if tool == "" {
			continue
		}
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(err)
		}
	}
	for _, test := range []struct {
		name    string
		project string
	}{
		{"languages keyword", "project(conly LANGUAGES C)"},
		{"legacy form", "project(conly C)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := t.TempDir()
			cmakelists := "cmake_minimum_required(VERSION 3.20)\n" + test.project + "\n"
			if err := os.WriteFile(filepath.Join(source, "CMakeLists.txt"), []byte(cmakelists), 0644); err != nil {
				t.Fatal(err)
			}
			p := NewPackage().SetDirs(PkgDirs{SourceDir: source, BuildDir: t.TempDir()}).SetToolchain(tc)
			vlog.SetLevel(vlog.Normal)
			defer vlog.SetLevel(vlog.Normal)
			_, errOut := captureCMakeStreams(t, func() { p.CMakeConfigure("-G", "Ninja") })
			for _, unwanted := range []string{"Manually-specified variables", "CMAKE_CXX_COMPILER", "CMAKE_ASM_COMPILER"} {
				if strings.Contains(errOut, unwanted) {
					t.Errorf("C-only configure stderr mentions %q:\n%s", unwanted, errOut)
				}
			}
		})
	}
}

func TestCMakeCapturedStagesCreateMissingBuildDirectory(t *testing.T) {
	fakeDir := t.TempDir()
	for _, stage := range []string{"configure", "build", "install"} {
		writeFakeCMakeStage(t, fakeDir, stage, "")
	}
	p := fakeCMakePackage(t, fakeDir)
	buildDir := filepath.Join(p.BuildDir(), "not", "yet", "created")
	p.SetDirs(PkgDirs{SourceDir: p.SourceDir(), BuildDir: buildDir})
	vlog.SetLevel(vlog.Quiet)
	defer vlog.SetLevel(vlog.Normal)
	var scriptErr *BuildScriptError
	out, errOut := captureCMakeStreams(t, func() {
		scriptErr = captureScriptError(func() {
			p.CMakeConfigure("-G", "Ninja")
			p.CMakeBuild()
			p.CMakeInstall()
		})
	})
	if scriptErr != nil {
		t.Fatalf("captured CMake failed: %v\nstdout:\n%s\nstderr:\n%s", scriptErr, out, errOut)
	}
	for _, stage := range []string{"configure", "build", "install"} {
		data, err := os.ReadFile(filepath.Join(fakeDir, stage+".cwd"))
		if err != nil {
			t.Fatalf("%s did not run: %v", stage, err)
		}
		if got := string(data); got != buildDir {
			t.Errorf("%s working directory = %q, want %q", stage, got, buildDir)
		}
	}
}

func TestCMakeBuildAndInstallOverrides(t *testing.T) {
	for _, key := range []string{"MAKEFLAGS", "MFLAGS", "GNUMAKEFLAGS", "CMAKE_BUILD_PARALLEL_LEVEL"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	p := NewPackage().SetDirs(PkgDirs{BuildDir: t.TempDir()}).SetCMakeBuildType("MinSizeRel")
	if err := BindBuildRuntime(p, &buildruntime.Budget{Jobs: 4}, nil); err != nil {
		t.Fatal(err)
	}
	args, err := p.cmakeBuildArgs()
	if err != nil {
		t.Fatal(err)
	}
	if i := slices.Index(args, "--parallel"); i < 0 || args[i+1] != "4" {
		t.Fatalf("bound parallelism = %q", args)
	}
	for _, parallel := range [][]string{{"-j", "2"}, {"-j2"}, {"--parallel", "2"}, {"--parallel=2"}} {
		extra := append([]string{"--config", "Debug"}, parallel...)
		args, err := p.cmakeBuildArgs(extra...)
		want := []string{"--build", filepath.ToSlash(p.CMakeBuildDir()), "--parallel", "2", "--config", "Debug"}
		if err != nil || !slices.Equal(args, want) {
			t.Fatalf("explicit arguments = %q, want %q, error %v", args, want, err)
		}
	}
	args, err = p.cmakeBuildArgs("--", "--parallel=1", "--config", "backend-value")
	if err != nil || !slices.Contains(args, "--parallel") || !slices.Contains(args, "MinSizeRel") {
		t.Fatalf("backend args suppressed CMake defaults: %q, %v", args, err)
	}
	t.Setenv("CMAKE_BUILD_PARALLEL_LEVEL", "2")
	args, err = p.cmakeBuildArgs()
	if i := slices.Index(args, "--parallel"); err != nil || i < 0 || args[i+1] != "2" {
		t.Fatalf("smaller parallel environment ignored: %q, %v", args, err)
	}
	for _, value := range []string{"5", "", "0", "-1"} {
		t.Setenv("CMAKE_BUILD_PARALLEL_LEVEL", value)
		if args, err = p.cmakeBuildArgs(); err == nil {
			t.Fatalf("invalid parallel environment %q accepted: %q", value, args)
		}
	}
	args, err = p.cmakeInstallArgs("--config=Debug", "--component", "Development")
	want := []string{"--install", filepath.ToSlash(p.CMakeBuildDir()), "--prefix", filepath.ToSlash(p.CMakeInstallDir()), "--config=Debug", "--component", "Development"}
	if err != nil || !slices.Equal(args, want) {
		t.Fatalf("install arguments = %q, want %q, error %v", args, want, err)
	}
}

func TestCMakeBuildRejectsPresetsThatOverrideManagedDirectory(t *testing.T) {
	p := NewPackage().SetDirs(PkgDirs{BuildDir: t.TempDir()})
	for _, extra := range [][]string{{"--preset", "other"}, {"--preset=other"}} {
		_, err := p.cmakeBuildArgs(extra...)
		if err == nil || !strings.Contains(err.Error(), "CMakeConfigure") || !strings.Contains(err.Error(), "--target or --config") {
			t.Errorf("build preset %q error = %v", extra, err)
		}
	}
	backend := []string{"--", "--preset", "backend-value"}
	args, err := p.cmakeBuildArgs(backend...)
	if err != nil || !slices.Equal(args[len(args)-len(backend):], backend) {
		t.Fatalf("backend arguments changed: %q, %v", args, err)
	}
}

func TestCMakeInheritsOnlyGlobalFlagsAndAllowsOverrides(t *testing.T) {
	p := NewPackage().SetToolchain(toolchain.GetBuiltinHost()).SetGlobalFlags([]string{"-DGLOBAL_C", "-mcpu=cortex-m4"}, []string{"-DGLOBAL_CXX"}, []string{"-Wl,--gc-sections"}, nil)
	args, err := p.cmakeConfigureArgs(runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range p.CMakeGlobalFlagsArgs() {
		if !slices.Contains(args, want) {
			t.Errorf("missing global argument %q: %q", want, args)
		}
	}
	for _, arg := range args {
		if strings.Contains(arg, "-Werror") || strings.Contains(arg, "-pie") || strings.HasPrefix(arg, "-DCMAKE_ASM_FLAGS=") {
			t.Errorf("unexpected automatic flags: %q", arg)
		}
	}
	extra := []string{"-D", "CMAKE_C_FLAGS:STRING=-DPROJECT_C", "-DCMAKE_SHARED_LINKER_FLAGS=", "-D", "CMAKE_MODULE_LINKER_FLAGS:STRING=-lproject", "-DCMAKE_ASM_FLAGS=-mthumb"}
	args, err = p.cmakeConfigureArgs(runtime.GOOS, extra...)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-DCMAKE_C_FLAGS=") || arg == "-DCMAKE_SHARED_LINKER_FLAGS=-Wl,--gc-sections" || arg == "-DCMAKE_MODULE_LINKER_FLAGS=-Wl,--gc-sections" {
			t.Errorf("explicit flag override retained automatic value: %q", arg)
		}
	}
	if !slices.Equal(args[len(args)-len(extra):], extra) || !slices.Contains(args, "-DCMAKE_EXE_LINKER_FLAGS=-Wl,--gc-sections") {
		t.Fatalf("flag overrides lost unrelated arguments: %q", args)
	}
}

func TestCMakePackageVisibilityAndOverrides(t *testing.T) {
	cflags := []string{"-DGLOBAL_C"}
	cxxflags := []string{"-DGLOBAL_CXX"}
	app := NewPackage().SetToolchain(&toolchain.Toolchain{}).
		SetGlobalFlags(cflags, cxxflags, nil, nil)
	dep := NewPackage().SetToolchain(&toolchain.Toolchain{}).
		SetGlobalFlags(cflags, cxxflags, nil, nil)
	NewConfigContextWithPackage("app", app).SetDefaultVisibilityHidden()
	for _, test := range []struct {
		name string
		pkg  *Package
		c    string
		cxx  string
	}{
		{"app", app, "-fvisibility=hidden -DGLOBAL_C", "-fvisibility=hidden -fvisibility-inlines-hidden -DGLOBAL_CXX"},
		{"dep", dep, "-DGLOBAL_C", "-DGLOBAL_CXX"},
	} {
		t.Run(test.name, func(t *testing.T) {
			args, err := test.pkg.cmakeConfigureArgs(runtime.GOOS)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"-DCMAKE_C_FLAGS=" + test.c, "-DCMAKE_CXX_FLAGS=" + test.cxx} {
				if !slices.Contains(args, want) {
					t.Errorf("missing argument %q in %q", want, args)
				}
			}
			if got := test.pkg.MergedCFlags("-fvisibility=default"); got != test.c+" -fvisibility=default" {
				t.Errorf("merged C flags = %q", got)
			}
			if got := test.pkg.MergedCxxFlags("-fno-visibility-inlines-hidden"); got != test.cxx+" -fno-visibility-inlines-hidden" {
				t.Errorf("merged CXX flags = %q", got)
			}
			if !slices.Equal(test.pkg.GlobalCFlags(), cflags) || !slices.Equal(test.pkg.GlobalCxxFlags(), cxxflags) {
				t.Fatal("package visibility changed global flags")
			}
		})
	}
	args, err := app.cmakeConfigureArgs(runtime.GOOS, "-DCMAKE_C_FLAGS=-fvisibility=default", "-D", "CMAKE_CXX_FLAGS:STRING=")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(args, "-DCMAKE_C_FLAGS=-fvisibility=default") || !slices.Contains(args, "CMAKE_CXX_FLAGS:STRING=") {
		t.Fatalf("explicit overrides lost: %q", args)
	}
	for _, arg := range args {
		if strings.Contains(arg, "visibility=hidden") || strings.Contains(arg, "visibility-inlines-hidden") {
			t.Fatalf("package default overrode explicit flags: %q", args)
		}
	}
}

func TestCMakeMissingConfiguredBinutilsFails(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		key   string
		tools toolchain.Tools
	}{
		{"CMAKE_AR", toolchain.Tools{AR: "missing-ar"}},
		{"CMAKE_RANLIB", toolchain.Tools{RANLIB: "missing-ranlib"}},
		{"CMAKE_CXX_COMPILER", toolchain.Tools{CXX: "missing-cxx"}},
	} {
		p := NewPackage().SetToolchain(&toolchain.Toolchain{InstallPath: root, Tools: test.tools})
		_, err := p.cmakeConfigureArgs(runtime.GOOS)
		var lookupErr *exec.Error
		if err == nil || !strings.Contains(err.Error(), test.key) || !errors.As(err, &lookupErr) || !strings.HasPrefix(lookupErr.Name, filepath.Join(root, "bin")+string(filepath.Separator)) {
			t.Errorf("%s missing tool error = %v", test.key, err)
		}
	}
}

func TestCMakeSettersAndCommandsReportScriptErrors(t *testing.T) {
	p := NewPackage().SetName("cmake-fixture").SetDryRun(true)
	for name, run := range map[string]func(){
		"SetCMakeBuildDir":   func() { p.SetCMakeBuildDir("") },
		"SetCMakeInstallDir": func() { p.SetCMakeInstallDir("") },
		"SetCMakeBuildType":  func() { p.SetCMakeBuildType(" ") },
		"CMakeConfigure":     func() { p.CMakeConfigure("-Bother") },
		"CMakeBuild":         func() { p.CMakeBuild("--build", "other") },
		"CMakeInstall":       func() { p.CMakeInstall("--prefix=other") },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				err, ok := recover().(*BuildScriptError)
				if !ok || err.Package != p.Name || err.Op != name {
					t.Fatalf("expected %s script error, got %v", name, err)
				}
			}()
			run()
			t.Fatal("expected script error")
		})
	}
}

func TestCMakeDryRunDoesNotExecuteOrCreateDirectories(t *testing.T) {
	root := t.TempDir()
	p := NewPackage().SetDirs(PkgDirs{SourceDir: filepath.Join(root, "missing-source"), BuildDir: filepath.Join(root, "missing-build")}).
		SetToolchain(&toolchain.Toolchain{}).SetDryRun(true)
	t.Setenv("PATH", "")
	out := captureCMakeStdout(t, func() {
		p.CMakeConfigure()
		p.CMakeConfigure("--preset", "fixture")
		p.CMakeBuild()
		p.CMakeInstall("--component", "Development")
	})
	for _, want := range []string{"  cmake -S ", " -B ", "  cmake --build ", "  cmake --install ", "--component Development"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run stdout missing %q:\n%s", want, out)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("dry-run created files: %v, %v", entries, err)
	}
}

func fakeCMakePackage(t *testing.T, fakeDir string) *Package {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(program)
	if err != nil {
		t.Fatal(err)
	}
	name := "cmake"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(bin, name), data, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VMAKE_TEST_CMAKE_FAKE_DIR", fakeDir)
	return NewPackage().SetName("cmake-fixture").
		SetDirs(PkgDirs{SourceDir: t.TempDir(), BuildDir: t.TempDir()}).
		SetToolchain(&toolchain.Toolchain{})
}

func writeFakeCMakeStage(t *testing.T, dir, stage, stdout string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, stage+".out"), []byte(stdout), 0644); err != nil {
		t.Fatal(err)
	}
}

func captureCMakeStdout(t *testing.T, fn func()) string {
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

func captureCMakeStreams(t *testing.T, fn func()) (string, string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	doneOut := make(chan string, 1)
	doneErr := make(chan string, 1)
	go func() {
		defer outR.Close()
		var sb strings.Builder
		_, _ = io.Copy(&sb, outR)
		doneOut <- sb.String()
	}()
	go func() {
		defer errR.Close()
		var sb strings.Builder
		_, _ = io.Copy(&sb, errR)
		doneErr <- sb.String()
	}()
	defer func() {
		os.Stdout, os.Stderr = oldOut, oldErr
	}()
	fn()
	_ = outW.Close()
	_ = errW.Close()
	return <-doneOut, <-doneErr
}

func TestCMakeBuildInstallOutputCaptured(t *testing.T) {
	tests := []struct {
		name       string
		stage      string
		output     string
		run        func(*Package)
		summary    string
		wantDetail []string
		wantStderr []string
		wantHidden []string
	}{
		{
			name:       "build progress",
			stage:      "build",
			output:     "[1/2] Building C object CMakeFiles/a.dir/a.c.o\n[2/2] Linking C static library liba.a\n",
			run:        func(p *Package) { p.CMakeBuild() },
			summary:    "  cmake build: 2 steps",
			wantHidden: []string{"[1/2]", "[2/2]"},
		},
		{
			name:       "build makefile generator progress",
			stage:      "build",
			output:     "[ 50%] Building C object CMakeFiles/a.dir/a.c.o\n[100%] Linking C static library liba.a\n",
			run:        func(p *Package) { p.CMakeBuild() },
			summary:    "  cmake build: 2 steps",
			wantHidden: []string{"[ 50%]", "[100%]"},
		},
		{
			name:       "build up to date",
			stage:      "build",
			output:     "ninja: no work to do.\n",
			run:        func(p *Package) { p.CMakeBuild() },
			summary:    "  cmake build: up to date",
			wantHidden: []string{"ninja: no work to do"},
		},
		{
			name:       "build warnings use stderr",
			stage:      "build",
			output:     "[1/1] Building C object a.c.o\nCMake Warning:\n  unused variable\nsrc/a.c:3:1: warning: unused parameter 'x'\n",
			run:        func(p *Package) { p.CMakeBuild() },
			summary:    "  cmake build: 1 step",
			wantStderr: []string{"CMake Warning:", "  unused variable", "src/a.c:3:1: warning: unused parameter 'x'"},
			wantHidden: []string{"[1/1]"},
		},
		{
			name:       "install summary and installed files",
			stage:      "install",
			output:     "-- Installing: /tmp/stage/lib/liba.a\n-- Up-to-date: /tmp/stage/include/a.h\n-- Installing: /tmp/stage/share/doc\n",
			run:        func(p *Package) { p.CMakeInstall() },
			summary:    "  cmake install: 2 installed, 1 up to date",
			wantDetail: []string{"-- Installing: /tmp/stage/lib/liba.a", "-- Installing: /tmp/stage/share/doc"},
			wantHidden: []string{"-- Up-to-date:"},
		},
		{
			name:       "install up to date",
			stage:      "install",
			output:     "-- Up-to-date: /tmp/stage/lib/liba.a\n-- Up-to-date: /tmp/stage/include/a.h\n",
			run:        func(p *Package) { p.CMakeInstall() },
			summary:    "  cmake install: 0 installed, 2 up to date",
			wantHidden: []string{"-- Up-to-date:"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fakeDir := t.TempDir()
			writeFakeCMakeStage(t, fakeDir, test.stage, test.output)
			p := fakeCMakePackage(t, fakeDir)
			vlog.SetLevel(vlog.Normal)
			defer vlog.SetLevel(vlog.Normal)
			out, errOut := captureCMakeStreams(t, func() { test.run(p) })
			if !strings.Contains(out, test.summary) {
				t.Fatalf("stdout missing summary %q:\n%s", test.summary, out)
			}
			for _, want := range test.wantDetail {
				if !strings.Contains(out, want) {
					t.Errorf("stdout missing detail %q:\n%s", want, out)
				}
			}
			for _, want := range test.wantStderr {
				if !strings.Contains(errOut, want) {
					t.Errorf("stderr missing diagnostic %q:\n%s", want, errOut)
				}
				if strings.Contains(out, want) {
					t.Errorf("stdout contains stderr line %q:\n%s", want, out)
				}
			}
			for _, hidden := range test.wantHidden {
				if strings.Contains(out, hidden) {
					t.Errorf("stdout contains hidden output %q:\n%s", hidden, out)
				}
			}
		})
	}
}

func TestCMakeConfigureOutputCaptured(t *testing.T) {
	fakeDir := t.TempDir()
	writeFakeCMakeStage(t, fakeDir, "configure", strings.Join([]string{
		"-- The C compiler identification is GNU 12.3.0",
		"-- Detecting C compiler ABI info",
		"-- Detecting C compiler ABI info - done",
		"-- Check for working C compiler: /usr/bin/gcc - skipped",
		"-- Configuring done (0.1s)",
		"-- Generating done (0.0s)",
		"-- Build files have been written to: /tmp/project/build/cmake",
		"",
	}, "\n"))
	p := fakeCMakePackage(t, fakeDir)
	vlog.SetLevel(vlog.Normal)
	defer vlog.SetLevel(vlog.Normal)
	out, errOut := captureCMakeStreams(t, func() { p.CMakeConfigure("-G", "Ninja") })
	if !strings.Contains(out, "  cmake configure: done") {
		t.Fatalf("stdout missing configure summary:\n%s", out)
	}
	for _, hidden := range []string{"-- The C compiler", "-- Detecting", "-- Configuring done", "-- Generating done", "-- Build files"} {
		if strings.Contains(out, hidden) {
			t.Errorf("stdout contains configure scaffolding %q:\n%s", hidden, out)
		}
	}
	if errOut != "" {
		t.Fatalf("configure success stderr = %q, want empty", errOut)
	}
}

func TestCMakeConfigureRoutesWarningsAndErrorsToStderr(t *testing.T) {
	fakeDir := t.TempDir()
	writeFakeCMakeStage(t, fakeDir, "configure", strings.Join([]string{
		"-- Configuring done (0.1s)",
		"CMake Warning at CMakeLists.txt:3 (message):",
		"  Manually-specified variables were not used",
		"src/a.c:3:1: warning: unused parameter 'x'",
		"",
	}, "\n"))
	p := fakeCMakePackage(t, fakeDir)
	vlog.SetLevel(vlog.Normal)
	defer vlog.SetLevel(vlog.Normal)
	out, errOut := captureCMakeStreams(t, func() { p.CMakeConfigure("-G", "Ninja") })
	for _, want := range []string{"CMake Warning at CMakeLists.txt:3 (message):", "  Manually-specified variables were not used", "src/a.c:3:1: warning: unused parameter 'x'"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing diagnostic %q:\n%s", want, errOut)
		}
		if strings.Contains(out, want) {
			t.Errorf("stdout contains diagnostic %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "  cmake configure: done") {
		t.Errorf("stdout missing configure summary:\n%s", out)
	}
}

func TestCMakeConfigureCapturedLogVisibleOnlyWhenVerbose(t *testing.T) {
	fakeDir := t.TempDir()
	writeFakeCMakeStage(t, fakeDir, "configure", "-- The C compiler identification is GNU 12.3.0\n-- Configuring done (0.1s)\n-- Build files have been written to: /tmp/build\n")
	p := fakeCMakePackage(t, fakeDir)
	vlog.SetLevel(vlog.VerboseLevel)
	defer vlog.SetLevel(vlog.Normal)
	out, _ := captureCMakeStreams(t, func() { p.CMakeConfigure("-G", "Ninja") })
	for _, want := range []string{"-- The C compiler identification is GNU 12.3.0", "-- Configuring done (0.1s)", "-- Build files have been written", "  cmake configure: done"} {
		if !strings.Contains(out, want) {
			t.Errorf("verbose stdout missing %q:\n%s", want, out)
		}
	}
}

func TestCMakeConfigureQuiet(t *testing.T) {
	fakeDir := t.TempDir()
	writeFakeCMakeStage(t, fakeDir, "configure", "-- Configuring done (0.1s)\nwarning: generic configure warning\n")
	p := fakeCMakePackage(t, fakeDir)
	vlog.SetLevel(vlog.Quiet)
	defer vlog.SetLevel(vlog.Normal)
	out, errOut := captureCMakeStreams(t, func() { p.CMakeConfigure("-G", "Ninja") })
	if out != "" {
		t.Fatalf("quiet stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "warning: generic configure warning") {
		t.Fatalf("quiet stderr missing warning:\n%s", errOut)
	}
}

func TestCMakeConfigureCachedSkipsExecution(t *testing.T) {
	fakeDir := t.TempDir()
	writeFakeCMakeStage(t, fakeDir, "configure", "-- Configuring done (0.1s)\n")
	p := fakeCMakePackage(t, fakeDir)
	logPath := filepath.Join(t.TempDir(), "cmake.log")
	t.Setenv("VMAKE_TEST_CMAKE_LOG", logPath)
	vlog.SetLevel(vlog.Normal)
	defer vlog.SetLevel(vlog.Normal)
	out, _ := captureCMakeStreams(t, func() { p.CMakeConfigure("-G", "Ninja") })
	if !strings.Contains(out, "  cmake configure: done") {
		t.Fatalf("first configure stdout = %q", out)
	}
	buildDir := p.CMakeBuildDir()
	if err := os.MkdirAll(filepath.Join(buildDir, "CMakeFiles"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"CMakeCache.txt", "build.ninja", "Makefile"} {
		if err := os.WriteFile(filepath.Join(buildDir, name), []byte("fixture\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	out, errOut := captureCMakeStreams(t, func() { p.CMakeConfigure("-G", "Ninja") })
	if !strings.Contains(out, "  cmake configure (cached): ") {
		t.Fatalf("cached configure stdout = %q", out)
	}
	if errOut != "" {
		t.Fatalf("cached configure stderr = %q", errOut)
	}
	if got := countCMakeConfigureCalls(t, logPath); got != 1 {
		t.Fatalf("cached configure ran cmake %d times, want 1", got)
	}
}

func TestCMakeConfigureWorkingDirectory(t *testing.T) {
	fakeDir := t.TempDir()
	writeFakeCMakeStage(t, fakeDir, "configure", "-- Configuring done (0.1s)\n")
	p := fakeCMakePackage(t, fakeDir)
	vlog.SetLevel(vlog.Quiet)
	defer vlog.SetLevel(vlog.Normal)
	configureDir := func() string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(fakeDir, "configure.cwd"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	p.CMakeConfigure("-G", "Ninja")
	if got, want := configureDir(), p.dirs.BuildDir; got != want {
		t.Fatalf("configure working directory = %q, want BuildDir %q", got, want)
	}
	p.CMakeConfigure("--preset", "fixture")
	if got, want := configureDir(), p.SrcDir(); got != want {
		t.Fatalf("preset configure working directory = %q, want SrcDir %q", got, want)
	}
}

func TestCMakeConfigureFailureKeepsCapturedOutput(t *testing.T) {
	fakeDir := t.TempDir()
	writeFakeCMakeStage(t, fakeDir, "configure", "-- Check for working C compiler: /usr/bin/gcc - broken\n-- Configuring incomplete, errors occurred!\n")
	if err := os.WriteFile(filepath.Join(fakeDir, "configure.err"), []byte("CMake Error at CMakeLists.txt:3 (message):\n  compiler does not work\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeDir, "configure.exit"), []byte("1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	p := fakeCMakePackage(t, fakeDir)
	vlog.SetLevel(vlog.Normal)
	defer vlog.SetLevel(vlog.Normal)
	var scriptErr *BuildScriptError
	out, errOut := captureCMakeStreams(t, func() {
		scriptErr = captureScriptError(func() { p.CMakeConfigure("-G", "Ninja") })
	})
	if scriptErr == nil || scriptErr.Package != p.Name || scriptErr.Op != "CMakeConfigure" {
		t.Fatalf("script error = %+v, want package %q op CMakeConfigure", scriptErr, p.Name)
	}
	for _, want := range []string{"CMake Error at CMakeLists.txt:3 (message):", "  compiler does not work"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing captured error %q:\n%s", want, errOut)
		}
	}
	if out != "" {
		t.Errorf("default configure failure stdout = %q, want empty", out)
	}
	for _, want := range []string{"CMake Error at CMakeLists.txt:3 (message):", "compiler does not work", "Configuring incomplete"} {
		if !strings.Contains(scriptErr.Error(), want) {
			t.Errorf("script error missing captured output %q:\n%s", want, scriptErr.Error())
		}
	}
	vlog.SetLevel(vlog.VerboseLevel)
	verboseOut, _ := captureCMakeStreams(t, func() { scriptErr = captureScriptError(func() { p.CMakeConfigure("-G", "Ninja") }) })
	for _, want := range []string{"-- Check for working C compiler: /usr/bin/gcc - broken", "-- Configuring incomplete, errors occurred!"} {
		if !strings.Contains(verboseOut, want) {
			t.Errorf("verbose failure stdout missing %q:\n%s", want, verboseOut)
		}
	}
}

func TestCMakeCapturedLogVisibleOnlyWhenVerbose(t *testing.T) {
	fakeDir := t.TempDir()
	writeFakeCMakeStage(t, fakeDir, "build", "[1/2] Building C object a.c.o\n[2/2] Linking C static library liba.a\n")
	p := fakeCMakePackage(t, fakeDir)
	vlog.SetLevel(vlog.VerboseLevel)
	defer vlog.SetLevel(vlog.Normal)
	out := captureCMakeStdout(t, func() { p.CMakeBuild() })
	for _, want := range []string{"[1/2] Building C object a.c.o", "[2/2] Linking C static library liba.a", "  cmake build: 2 steps"} {
		if !strings.Contains(out, want) {
			t.Errorf("verbose stdout missing %q:\n%s", want, out)
		}
	}
}

func TestCMakeCapturedOutputQuiet(t *testing.T) {
	fakeDir := t.TempDir()
	writeFakeCMakeStage(t, fakeDir, "install", "-- Installing: /tmp/stage/lib/liba.a\n")
	p := fakeCMakePackage(t, fakeDir)
	vlog.SetLevel(vlog.Quiet)
	defer vlog.SetLevel(vlog.Normal)
	out := captureCMakeStdout(t, func() { p.CMakeInstall() })
	if out != "" {
		t.Fatalf("quiet stdout = %q, want empty", out)
	}
}

func TestCMakeFailureKeepsFullCapturedOutput(t *testing.T) {
	for _, stage := range []string{"build", "install"} {
		t.Run(stage, func(t *testing.T) {
			fakeDir := t.TempDir()
			writeFakeCMakeStage(t, fakeDir, stage, "[1/1] Building C object a.c.o\nFAILED: a.c.o\ncompiler exploded\n")
			if err := os.WriteFile(filepath.Join(fakeDir, stage+".exit"), []byte("1\n"), 0644); err != nil {
				t.Fatal(err)
			}
			p := fakeCMakePackage(t, fakeDir)
			vlog.SetLevel(vlog.Normal)
			defer vlog.SetLevel(vlog.Normal)
			run := func() {
				if stage == "build" {
					p.CMakeBuild()
				} else {
					p.CMakeInstall()
				}
			}
			var scriptErr *BuildScriptError
			out := captureCMakeStdout(t, func() {
				scriptErr = captureScriptError(run)
			})
			if scriptErr == nil {
				t.Fatal("expected a script error")
			}
			for _, want := range []string{"FAILED: a.c.o", "compiler exploded"} {
				if !strings.Contains(out, want) {
					t.Errorf("default failure stdout missing captured output %q:\n%s", want, out)
				}
			}
			if strings.Contains(out, "[1/1]") {
				t.Errorf("default failure stdout contains progress line:\n%s", out)
			}
			wantOp := "CMakeBuild"
			if stage == "install" {
				wantOp = "CMakeInstall"
			}
			if scriptErr.Package != p.Name || scriptErr.Op != wantOp {
				t.Fatalf("script error = %+v, want package %q op %q", scriptErr, p.Name, wantOp)
			}
			for _, want := range []string{"[1/1] Building C object a.c.o", "FAILED: a.c.o", "compiler exploded"} {
				if !strings.Contains(scriptErr.Error(), want) {
					t.Errorf("error missing captured output %q:\n%s", want, scriptErr.Error())
				}
			}
			vlog.SetLevel(vlog.VerboseLevel)
			verboseOut := captureCMakeStdout(t, func() { scriptErr = captureScriptError(run) })
			if !strings.Contains(verboseOut, "[1/1] Building C object a.c.o") {
				t.Errorf("verbose failure stdout missing captured log:\n%s", verboseOut)
			}
		})
	}
}

func captureScriptError(fn func()) (scriptErr *BuildScriptError) {
	defer func() {
		scriptErr, _ = recover().(*BuildScriptError)
	}()
	fn()
	return nil
}

func writeFakeCMakeSleep(t *testing.T, dir, stage string, delay time.Duration) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, stage+".sleep"), []byte(delay.String()), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCMakeLongStageHeartbeat(t *testing.T) {
	saved := cmakeHeartbeatInterval
	t.Cleanup(func() { cmakeHeartbeatInterval = saved })
	t.Cleanup(func() { vlog.SetLevel(vlog.Normal) })
	tests := []struct {
		name     string
		stage    string
		output   string
		run      func(*Package)
		interval time.Duration
		sleep    time.Duration
		quiet    bool
		summary  string
	}{
		{"build default", "build", "[1/1] Linking C static library liba.a\n", func(p *Package) { p.CMakeBuild() }, 50 * time.Millisecond, 400 * time.Millisecond, false, "  cmake build: 1 step"},
		{"configure quiet", "configure", "-- Configuring done (0.1s)\n", func(p *Package) { p.CMakeConfigure("-G", "Ninja") }, 50 * time.Millisecond, 400 * time.Millisecond, true, ""},
		{"install default", "install", "-- Installing: /tmp/stage/lib/liba.a\n", func(p *Package) { p.CMakeInstall() }, 50 * time.Millisecond, 400 * time.Millisecond, false, "  cmake install: 1 installed, 0 up to date"},
		{"build fast", "build", "[1/1] Linking C static library liba.a\n", func(p *Package) { p.CMakeBuild() }, 10 * time.Second, 0, false, "  cmake build: 1 step"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fakeDir := t.TempDir()
			writeFakeCMakeStage(t, fakeDir, test.stage, test.output)
			if test.sleep > 0 {
				writeFakeCMakeSleep(t, fakeDir, test.stage, test.sleep)
			}
			cmakeHeartbeatInterval = test.interval
			p := fakeCMakePackage(t, fakeDir)
			if test.quiet {
				vlog.SetLevel(vlog.Quiet)
			} else {
				vlog.SetLevel(vlog.Normal)
			}
			out, errOut := captureCMakeStreams(t, func() { test.run(p) })
			heartbeats := strings.Count(errOut, "  cmake "+test.stage+": still running (")
			if test.sleep == 0 {
				if heartbeats != 0 {
					t.Fatalf("fast stage printed %d heartbeats:\n%s", heartbeats, errOut)
				}
			} else if heartbeats < 2 {
				t.Fatalf("heartbeats = %d, want at least 2:\n%s", heartbeats, errOut)
			}
			if strings.Contains(out, "still running") {
				t.Fatalf("heartbeat printed to stdout:\n%s", out)
			}
			if test.summary == "" {
				if out != "" {
					t.Fatalf("quiet stdout = %q, want empty", out)
				}
			} else if !strings.Contains(out, test.summary) {
				t.Fatalf("stdout missing summary %q:\n%s", test.summary, out)
			}
		})
	}
}

func TestCMakeSlowStageFailureStaysBounded(t *testing.T) {
	saved := cmakeHeartbeatInterval
	t.Cleanup(func() { cmakeHeartbeatInterval = saved })
	t.Cleanup(func() { vlog.SetLevel(vlog.Normal) })
	cmakeHeartbeatInterval = 50 * time.Millisecond
	fakeDir := t.TempDir()
	writeFakeCMakeStage(t, fakeDir, "build", "[1/1] Building C object a.c.o\nFAILED: a.c.o\ncompiler exploded\n")
	writeFakeCMakeSleep(t, fakeDir, "build", 300*time.Millisecond)
	if err := os.WriteFile(filepath.Join(fakeDir, "build.exit"), []byte("1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	p := fakeCMakePackage(t, fakeDir)
	vlog.SetLevel(vlog.Normal)
	var scriptErr *BuildScriptError
	out, errOut := captureCMakeStreams(t, func() {
		scriptErr = captureScriptError(func() { p.CMakeBuild() })
	})
	if scriptErr == nil || scriptErr.Op != "CMakeBuild" {
		t.Fatalf("script error = %+v, want op CMakeBuild", scriptErr)
	}
	if heartbeats := strings.Count(errOut, "  cmake build: still running ("); heartbeats < 2 {
		t.Fatalf("failure heartbeats = %d, want at least 2:\n%s", heartbeats, errOut)
	}
	for _, want := range []string{"FAILED: a.c.o", "compiler exploded"} {
		if !strings.Contains(out, want) {
			t.Errorf("failure stdout missing %q:\n%s", want, out)
		}
		if !strings.Contains(scriptErr.Error(), want) {
			t.Errorf("script error missing %q:\n%s", want, scriptErr.Error())
		}
	}
	if strings.Contains(scriptErr.Error(), "still running") || strings.Contains(out, "still running") {
		t.Fatalf("heartbeat leaked into the failure summary:\nstdout:\n%s\nerror:\n%s", out, scriptErr.Error())
	}
}
