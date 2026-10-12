package api

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestMain(m *testing.M) {
	switch filepath.Base(os.Args[0]) {
	case "cmake", "cmake.exe":
		runCMakeWrapper()
	}
	os.Exit(m.Run())
}

func runCMakeWrapper() {
	real := os.Getenv("VMAKE_TEST_CMAKE_REAL")
	if real == "" {
		fmt.Fprintln(os.Stderr, "cmake wrapper: VMAKE_TEST_CMAKE_REAL is not set")
		os.Exit(2)
	}
	if logPath := os.Getenv("VMAKE_TEST_CMAKE_LOG"); logPath != "" {
		if log, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			fmt.Fprintln(log, strings.Join(os.Args[1:], " "))
			log.Close()
		}
	}
	cmd := exec.Command(real, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
	os.Exit(0)
}

func commandPathPackage(t *testing.T, installed bool) *Package {
	t.Helper()
	root := filepath.Join(t.TempDir(), "toolchain space")
	bin := filepath.Join(root, "bin")
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
	name := "vmake-path-probe"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(bin, name)
	if err := os.WriteFile(path, data, 0755); err != nil {
		t.Fatal(err)
	}
	tc := &toolchain.Toolchain{Tools: toolchain.Tools{CC: path}}
	if installed {
		tc.InstallPath = root
		tc.Tools.CC = name
	}
	dir := t.TempDir()
	return NewPackage().SetToolchain(tc).SetDirs(PkgDirs{SourceDir: dir, BuildDir: dir})
}

func TestPackageCommandsInheritToolchainPath(t *testing.T) {
	cmake, err := exec.LookPath("cmake")
	if err != nil {
		t.Skip(err)
	}
	t.Setenv("VMAKE_SHELL_TOOL_PROCESS", "1")
	for _, installed := range []bool{false, true} {
		name := "absolute compiler"
		if installed {
			name = "installed compiler"
		}
		t.Run(name, func(t *testing.T) {
			p := commandPathPackage(t, installed)
			inherited := os.Getenv("PATH")
			script := filepath.Join(p.SourceDir(), "probe.cmake")
			if err := os.WriteFile(script, []byte(`find_program(PROBE NAMES vmake-path-probe REQUIRED)
execute_process(COMMAND vmake-path-probe -test.run=^TestShellToolProcess$ -- "${OUTPUT}" COMMAND_ERROR_IS_FATAL ANY)
`), 0644); err != nil {
				t.Fatal(err)
			}
			for _, method := range []string{"Run", "RunIn", "RunEnv", "Exec"} {
				t.Run(method, func(t *testing.T) {
					output := filepath.Join(p.BuildDir(), method+".out")
					args := []string{"-DOUTPUT=" + filepath.ToSlash(output), "-P", filepath.ToSlash(script)}
					switch method {
					case "Run":
						p.Run(cmake, args...)
					case "RunIn":
						p.RunIn(t.TempDir(), cmake, args...)
					case "RunEnv":
						if err := p.RunEnv(map[string]string{"VMAKE_EXTRA_ENV": "1"}, cmake, args...); err != nil {
							t.Fatal(err)
						}
					case "Exec":
						work := t.TempDir()
						t.Chdir(work)
						output = filepath.Join(work, method+".out")
						args[0] = "-DOUTPUT=" + method + ".out"
						NewBuildContext("probe", nil).SetPackage(p).Exec(cmake, args...)
					}
					assertCommandPathOutput(t, output)
					if got := os.Getenv("PATH"); got != inherited {
						t.Fatalf("process PATH changed to %q", got)
					}
				})
			}
		})
	}
}

func TestPackageEnvPreservesBareCompilerSearchOrder(t *testing.T) {
	p := commandPathPackage(t, false)
	compiler := p.tc.Tools.CC
	p.tc.Tools.CC = filepath.Base(compiler)
	inherited := t.TempDir() + string(os.PathListSeparator) + filepath.Dir(compiler)
	t.Setenv("PATH", inherited)
	env := p.Env()
	if path, ok := env["PATH"]; ok {
		t.Fatalf("bare compiler changed inherited PATH %q to %q", inherited, path)
	}
	if env["CC"] != filepath.ToSlash(compiler) {
		t.Fatalf("CC = %q, want %q", env["CC"], compiler)
	}
}

func TestCMakePhasesInheritToolchainPath(t *testing.T) {
	for _, program := range []string{"cmake", "ninja"} {
		if _, err := exec.LookPath(program); err != nil {
			t.Skip(err)
		}
	}
	t.Setenv("VMAKE_SHELL_TOOL_PROCESS", "1")
	for _, installed := range []bool{false, true} {
		name := "absolute compiler"
		if installed {
			name = "installed compiler"
		}
		t.Run(name, func(t *testing.T) {
			p := commandPathPackage(t, installed)
			inherited := os.Getenv("PATH")
			project := `cmake_minimum_required(VERSION 3.20)
project(path_probe LANGUAGES NONE)
find_program(PROBE NAMES vmake-path-probe REQUIRED)
execute_process(COMMAND vmake-path-probe -test.run=^TestShellToolProcess$ -- "${CMAKE_BINARY_DIR}/configure.out" COMMAND_ERROR_IS_FATAL ANY)
add_custom_target(probe ALL COMMAND vmake-path-probe -test.run=^TestShellToolProcess$ -- "${CMAKE_BINARY_DIR}/build.out" VERBATIM)
install(CODE [[execute_process(COMMAND vmake-path-probe -test.run=^TestShellToolProcess$ -- "${CMAKE_CURRENT_LIST_DIR}/install.out" COMMAND_ERROR_IS_FATAL ANY)]])
`
			if err := os.WriteFile(filepath.Join(p.SourceDir(), "CMakeLists.txt"), []byte(project), 0644); err != nil {
				t.Fatal(err)
			}
			p.CMakeConfigure("-G", "Ninja")
			p.CMakeBuild()
			p.CMakeInstall()
			for _, phase := range []string{"configure", "build", "install"} {
				assertCommandPathOutput(t, filepath.Join(p.CMakeBuildDir(), phase+".out"))
			}
			if got := os.Getenv("PATH"); got != inherited {
				t.Fatalf("process PATH changed to %q", got)
			}
		})
	}
}

func assertCommandPathOutput(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "CONFIG_OK=y\n" {
		t.Fatalf("output %s = %q, %v", path, data, err)
	}
}

func TestCMakeConfigureSkipsUnchangedInputs(t *testing.T) {
	for _, program := range []string{"cmake", "ninja"} {
		if _, err := exec.LookPath(program); err != nil {
			t.Skip(err)
		}
	}
	realCMake, err := exec.LookPath("cmake")
	if err != nil {
		t.Skip(err)
	}
	root := t.TempDir()
	source := filepath.Join(root, "source")
	build := filepath.Join(root, "build")
	bin := filepath.Join(root, "bin")
	logPath := filepath.Join(root, "cmake.log")
	for _, dir := range []string{source, build, bin} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(program)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(bin, "cmake")
	if runtime.GOOS == "windows" {
		wrapper += ".exe"
	}
	if err := os.WriteFile(wrapper, data, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VMAKE_TEST_CMAKE_REAL", realCMake)
	t.Setenv("VMAKE_TEST_CMAKE_LOG", logPath)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	project := "cmake_minimum_required(VERSION 3.20)\nproject(fingerprint_probe LANGUAGES NONE)\n"
	cmakeLists := filepath.Join(source, "CMakeLists.txt")
	if err := os.WriteFile(cmakeLists, []byte(project), 0644); err != nil {
		t.Fatal(err)
	}
	p := NewPackage().SetDirs(PkgDirs{SourceDir: source, BuildDir: build}).SetToolchain(&toolchain.Toolchain{})
	p.CMakeConfigure("-G", "Ninja")
	if got := countCMakeConfigureCalls(t, logPath); got != 1 {
		t.Fatalf("first configure calls = %d, want 1", got)
	}
	p.CMakeConfigure("-G", "Ninja")
	if got := countCMakeConfigureCalls(t, logPath); got != 1 {
		t.Fatalf("unchanged configure calls = %d, want 1", got)
	}
	if err := os.WriteFile(cmakeLists, []byte(project+"# changed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	p.CMakeConfigure("-G", "Ninja")
	if got := countCMakeConfigureCalls(t, logPath); got != 2 {
		t.Fatalf("changed configure calls = %d, want 2", got)
	}
}

func countCMakeConfigureCalls(t *testing.T, logPath string) int {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(line, "-S ") {
			count++
		}
	}
	return count
}
