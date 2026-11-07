package cmake

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/toolchain"
)

func testSettings(t *testing.T) Settings {
	t.Helper()
	root := t.TempDir()
	return Settings{
		SourceDir:  filepath.Join(root, "source"),
		BuildRoot:  filepath.Join(root, "build"),
		BuildDir:   filepath.Join(root, "build", "cmake"),
		InstallDir: filepath.Join(root, "staging"),
		BuildType:  "Release",
		HostOS:     runtime.GOOS,
		TargetOS:   runtime.GOOS,
		Toolchain:  &toolchain.Toolchain{},
	}
}

func writeCMakeLists(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CMakeLists.txt"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func toolchainStub(t *testing.T) *toolchain.Toolchain {
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

func TestConfigureArgsManagedPaths(t *testing.T) {
	s := testSettings(t)
	args, err := ConfigureArgs(s, "-DEXTRA=1")
	if err != nil {
		t.Fatal(err)
	}
	prefix := []string{
		"-S", filepath.ToSlash(s.SourceDir),
		"-B", filepath.ToSlash(s.BuildDir),
		"-DCMAKE_INSTALL_PREFIX=" + filepath.ToSlash(s.InstallDir),
		"-DCMAKE_BUILD_TYPE=Release",
		"-DEXTRA=1",
	}
	if len(args) < len(prefix) || !slices.Equal(args[:len(prefix)], prefix) {
		t.Fatalf("configure arguments = %q, want prefix %q", args, prefix)
	}
}

func TestConfigureArgsRequiresToolchain(t *testing.T) {
	s := testSettings(t)
	s.Toolchain = nil
	if _, err := ConfigureArgs(s); err == nil || err.Error() != "toolchain is not configured" {
		t.Fatalf("nil toolchain error = %v", err)
	}
}

func TestConfigureArgsLanguageAwareCompilerInjection(t *testing.T) {
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
			name:       "cxx only",
			cmakelists: "cmake_minimum_required(VERSION 3.20)\nproject(cxxonly LANGUAGES CXX)\n",
			want:       []string{"CMAKE_CXX_COMPILER", "CMAKE_CXX_COMPILER_TARGET", "CMAKE_AR", "CMAKE_RANLIB", "CMAKE_STRIP", "CMAKE_OBJCOPY", "CMAKE_NM"},
			absent:     []string{"CMAKE_C_COMPILER", "CMAKE_ASM_COMPILER", "CMAKE_C_COMPILER_TARGET", "CMAKE_ASM_COMPILER_TARGET"},
		},
		{
			name:       "asm only",
			cmakelists: "project(asmonly LANGUAGES ASM)\n",
			want:       []string{"CMAKE_ASM_COMPILER", "CMAKE_ASM_COMPILER_TARGET", "CMAKE_AR", "CMAKE_RANLIB", "CMAKE_STRIP", "CMAKE_OBJCOPY", "CMAKE_NM"},
			absent:     []string{"CMAKE_C_COMPILER", "CMAKE_CXX_COMPILER", "CMAKE_C_COMPILER_TARGET", "CMAKE_CXX_COMPILER_TARGET"},
		},
		{
			name:       "multiline project",
			cmakelists: "cmake_minimum_required(VERSION 3.20)\nproject(\n  app\n  VERSION 1.0\n  LANGUAGES C CXX\n  DESCRIPTION \"multi line\"\n)\n",
			want:       []string{"CMAKE_C_COMPILER", "CMAKE_CXX_COMPILER", "CMAKE_C_COMPILER_TARGET", "CMAKE_CXX_COMPILER_TARGET"},
			absent:     []string{"CMAKE_ASM_COMPILER", "CMAKE_ASM_COMPILER_TARGET"},
		},
		{
			name:       "commented project is ignored",
			cmakelists: "# project(commented LANGUAGES C)\nproject(real LANGUAGES CXX) # trailing comment\n",
			want:       []string{"CMAKE_CXX_COMPILER", "CMAKE_CXX_COMPILER_TARGET"},
			absent:     []string{"CMAKE_C_COMPILER", "CMAKE_ASM_COMPILER", "CMAKE_C_COMPILER_TARGET", "CMAKE_ASM_COMPILER_TARGET"},
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
		{
			name:       "enable_language falls back to all compilers",
			cmakelists: "project(app LANGUAGES C)\nenable_language(ASM)\n",
			want:       allCompilers,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := testSettings(t)
			s.TargetTriple = "arm-none-eabi"
			s.Toolchain = toolchainStub(t)
			if test.cmakelists != "" {
				writeCMakeLists(t, s.SourceDir, test.cmakelists)
			}
			args, err := ConfigureArgs(s)
			if err != nil {
				t.Fatal(err)
			}
			definitions := Definitions(args)
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

func TestConfigureArgsTargetSystemAndGenerator(t *testing.T) {
	t.Setenv("CMAKE_GENERATOR", "")
	for _, test := range []struct {
		name, hostOS, targetOS, system, generator string
		extra                                     []string
	}{
		{"linux cross", "windows", "linux", "Linux", "Ninja", nil},
		{"windows cross", "linux", "windows", "Windows", "", nil},
		{"explicit generator", "windows", "none", "Generic", "Unix Makefiles", []string{"-G", "Unix Makefiles"}},
		{"compact generator", "windows", "none", "Generic", "-GMinGW Makefiles", []string{"-GMinGW Makefiles"}},
		{"preset generator", "windows", "none", "Generic", "", []string{"--preset=firmware"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := testSettings(t)
			s.HostOS = test.hostOS
			s.TargetOS = test.targetOS
			args, err := ConfigureArgs(s, test.extra...)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(args, "-DCMAKE_SYSTEM_NAME="+test.system) {
				t.Errorf("system name missing: %q", args)
			}
			if test.generator != "" && !slices.Contains(args, test.generator) {
				t.Errorf("generator missing: %q", args)
			}
			if test.generator != "Ninja" && slices.Contains(args, "Ninja") {
				t.Errorf("unexpected default generator: %q", args)
			}
		})
	}
	if _, err := ConfigureArgs(Settings{HostOS: "linux", TargetOS: "plan9", Toolchain: &toolchain.Toolchain{}}); err == nil || err.Error() != `unsupported CMake target OS "plan9"` {
		t.Fatalf("unsupported target OS error = %v", err)
	}
}

func TestConfigureArgsBareMetal(t *testing.T) {
	s := testSettings(t)
	s.TargetOS = "none"
	s.TargetTriple = "arm-none-eabi"
	s.Toolchain = toolchainStub(t)
	args, err := ConfigureArgs(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"-DCMAKE_SYSTEM_NAME=Generic",
		"-DCMAKE_TRY_COMPILE_TARGET_TYPE=STATIC_LIBRARY",
		"-DCMAKE_FIND_ROOT_PATH_MODE_PROGRAM=NEVER",
		"-DCMAKE_FIND_ROOT_PATH_MODE_LIBRARY=ONLY",
		"-DCMAKE_FIND_ROOT_PATH_MODE_INCLUDE=ONLY",
		"-DCMAKE_C_COMPILER_TARGET=arm-none-eabi",
		"-DCMAKE_CXX_COMPILER_TARGET=arm-none-eabi",
		"-DCMAKE_ASM_COMPILER_TARGET=arm-none-eabi",
	} {
		if !slices.Contains(args, want) {
			t.Errorf("missing %q in %q", want, args)
		}
	}
}

func TestConfigureArgsGlobalFlagsAndOverrides(t *testing.T) {
	s := testSettings(t)
	s.Toolchain = toolchainStub(t)
	s.GlobalFlags = []string{
		"-DCMAKE_C_FLAGS=-DGLOBAL_C",
		"-DCMAKE_CXX_FLAGS=-DGLOBAL_CXX",
		"-DCMAKE_EXE_LINKER_FLAGS=-Wl,--gc-sections",
	}
	writeCMakeLists(t, s.SourceDir, "project(conly LANGUAGES C)\n")
	args, err := ConfigureArgs(s)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(args, "-DCMAKE_C_FLAGS=-DGLOBAL_C") {
		t.Errorf("C flags missing: %q", args)
	}
	if slices.Contains(args, "-DCMAKE_CXX_FLAGS=-DGLOBAL_CXX") {
		t.Errorf("CXX flags injected for a C-only project: %q", args)
	}
	if !slices.Contains(args, "-DCMAKE_EXE_LINKER_FLAGS=-Wl,--gc-sections") {
		t.Errorf("linker flags missing: %q", args)
	}
	extra := []string{"-D", "CMAKE_C_FLAGS:STRING=-DPROJECT_C", "-DCMAKE_SHARED_LINKER_FLAGS=", "-D", "CMAKE_MODULE_LINKER_FLAGS:STRING=-lproject"}
	args, err = ConfigureArgs(s, extra...)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(args, "-DCMAKE_C_FLAGS=-DGLOBAL_C") {
		t.Errorf("explicit override retained the automatic C flags: %q", args)
	}
	if !slices.Equal(args[len(args)-len(extra):], extra) {
		t.Fatalf("extra arguments changed: %q", args)
	}
}

func TestConfigureArgsMissingConfiguredTool(t *testing.T) {
	root := t.TempDir()
	s := testSettings(t)
	s.Toolchain = &toolchain.Toolchain{InstallPath: root, Tools: toolchain.Tools{AR: "missing-ar"}}
	_, err := ConfigureArgs(s)
	if err == nil || !strings.Contains(err.Error(), "CMAKE_AR") || !strings.Contains(err.Error(), filepath.Join(root, "bin")) {
		t.Fatalf("missing tool error = %v", err)
	}
}

func TestConfigureDir(t *testing.T) {
	s := testSettings(t)
	if got := ConfigureDir(s, nil); got != s.BuildRoot {
		t.Fatalf("configure directory = %q, want build root %q", got, s.BuildRoot)
	}
	if got := ConfigureDir(s, []string{"--preset=release"}); got != s.SourceDir {
		t.Fatalf("preset configure directory = %q, want source %q", got, s.SourceDir)
	}
}

func TestValidateArgs(t *testing.T) {
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
		err := ValidateArgs(test.args)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("args %q: error = %v, want %s", test.args, err, test.want)
		}
	}
	backend := []string{"--", "-B", "--prefix=backend-value", "-DCMAKE_BUILD_TYPE=backend-value"}
	if err := ValidateArgs(backend); err != nil {
		t.Fatalf("backend arguments rejected: %v", err)
	}
}

func TestHasOptionAndDefinitions(t *testing.T) {
	args := []string{"-G", "Ninja", "-D", "CMAKE_C_FLAGS:STRING=-DPROJECT", "-DCMAKE_BUILD_TYPE=Release", "--", "-B", "backend"}
	if !HasGenerator(args) {
		t.Error("HasGenerator = false")
	}
	if !HasOption(args, "-G") {
		t.Error("HasOption missed -G")
	}
	if HasOption(args, "-B") {
		t.Error("HasOption scanned past --")
	}
	definitions := Definitions(args)
	if !definitions["CMAKE_C_FLAGS"] || !definitions["CMAKE_BUILD_TYPE"] || definitions["B"] {
		t.Fatalf("definitions = %#v", definitions)
	}
	if got := DefinitionKey("CMAKE_C_FLAGS:STRING=-DPROJECT"); got != "CMAKE_C_FLAGS" {
		t.Fatalf("DefinitionKey = %q", got)
	}
}

func TestProjectLanguages(t *testing.T) {
	for _, test := range []struct {
		name       string
		cmakelists string
		want       []string
		ok         bool
	}{
		{"languages", "project(app LANGUAGES C CXX)\n", []string{"C", "CXX"}, true},
		{"legacy", "project(app C Fortran)\n", []string{"C", "FORTRAN"}, true},
		{"keywords stop the list", "project(app LANGUAGES C VERSION 1.0 DESCRIPTION x)\n", []string{"C"}, true},
		{"variable is unparseable", "project(app LANGUAGES ${LANGS})\n", nil, false},
		{"enable language", "project(app LANGUAGES C)\nenable_language(ASM)\n", nil, false},
		{"dialect is unparseable", "project(app LANGUAGES C ASM-ATT)\n", nil, false},
		{"list separator is unparseable", "project(app LANGUAGES C;D)\n", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			writeCMakeLists(t, dir, test.cmakelists)
			languages, ok := ProjectLanguages(dir)
			if ok != test.ok {
				t.Fatalf("ok = %v, want %v (languages %#v)", ok, test.ok, languages)
			}
			for _, language := range test.want {
				if !languages[language] {
					t.Errorf("missing language %q in %#v", language, languages)
				}
			}
		})
	}
	if _, ok := ProjectLanguages(filepath.Join(t.TempDir(), "missing")); ok {
		t.Fatal("missing CMakeLists.txt reported a language list")
	}
}
