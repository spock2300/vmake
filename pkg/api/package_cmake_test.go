package api

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/buildruntime"
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
			for _, mode := range []string{ModeDebug, ModeRelease} {
				p.CfgVals = map[string]any{ModeOptionName: mode}
				config := "Release"
				if mode == ModeDebug {
					config = "Debug"
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
	p.CMakeConfigure()
	p.CMakeConfigure("--preset", "fixture")
	p.CMakeBuild()
	p.CMakeInstall("--component", "Development")
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("dry-run created files: %v, %v", entries, err)
	}
}
