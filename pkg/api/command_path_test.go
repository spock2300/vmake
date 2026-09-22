package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spock2300/vmake/pkg/toolchain"
)

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
