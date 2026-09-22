package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestCMakeBareMetalUsesConfiguredTools(t *testing.T) {
	t.Setenv("CMAKE_GENERATOR", "")
	root := filepath.Join(t.TempDir(), "Arm tools")
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	tool := "arm-none-eabi-gcc"
	if runtime.GOOS == "windows" {
		tool += ".exe"
	}
	toolPath := filepath.Join(bin, tool)
	if err := os.WriteFile(toolPath, []byte("tool"), 0755); err != nil {
		t.Fatal(err)
	}
	p := NewPackage().SetToolchain(&toolchain.Toolchain{
		TargetOS: "none", TargetTriple: "arm-none-eabi", InstallPath: root, Prefix: "arm-none-eabi-",
		Tools: toolchain.Tools{CC: tool, CXX: tool, AR: tool, LD: tool, RANLIB: tool, MAKE: tool},
	}).SetDirs(PkgDirs{SourceDir: root, BuildDir: filepath.Join(root, "build"), InstallDir: filepath.Join(root, "install")})
	args, err := p.cmakeConfigureArgs("windows")
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
		"-DCMAKE_ASM_COMPILER_TARGET=arm-none-eabi",
		"-DCMAKE_C_COMPILER=" + filepath.ToSlash(toolPath),
		"-DCMAKE_ASM_COMPILER=" + filepath.ToSlash(toolPath),
		"-DCMAKE_AR=" + filepath.ToSlash(toolPath),
		"-DCMAKE_RANLIB=" + filepath.ToSlash(toolPath),
		"Ninja",
	} {
		if !slices.Contains(args, want) {
			t.Errorf("missing %q in %q", want, args)
		}
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-DCMAKE_LINKER=") {
			t.Errorf("CMake linker must be discovered by CMake: %q", arg)
		}
	}
	if p.TargetTriple() != "arm-none-eabi" {
		t.Fatalf("TargetTriple = %q", p.TargetTriple())
	}
	env := p.Env()
	for _, key := range []string{"CC", "CXX", "AR", "RANLIB", "MAKE"} {
		if env[key] != filepath.ToSlash(toolPath) {
			t.Errorf("env %s = %q", key, env[key])
		}
	}
	if want := filepath.ToSlash(filepath.Join(bin, "arm-none-eabi-")); env["CROSS_COMPILE"] != want {
		t.Errorf("CROSS_COMPILE = %q, want %q", env["CROSS_COMPILE"], want)
	}
	if p.makeTool() != toolPath {
		t.Errorf("make program = %q", p.makeTool())
	}
	if p.tc.Tools.CC != tool {
		t.Fatal("Env changed the shared toolchain")
	}
}

func TestCMakeTargetSystemAndGenerator(t *testing.T) {
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
			p := NewPackage().SetToolchain(&toolchain.Toolchain{TargetOS: test.targetOS})
			args, err := p.cmakeConfigureArgs(test.hostOS, test.extra...)
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
	t.Setenv("CMAKE_GENERATOR", "Unix Makefiles")
	p := NewPackage().SetToolchain(&toolchain.Toolchain{TargetOS: "none"})
	args, err := p.cmakeConfigureArgs("windows")
	if err != nil || slices.Contains(args, "Ninja") {
		t.Fatalf("environment generator overridden: %q, %v", args, err)
	}
}

func TestCMakeMissingConfiguredCompilerFails(t *testing.T) {
	p := NewPackage().SetToolchain(&toolchain.Toolchain{TargetOS: "none", Tools: toolchain.Tools{CC: filepath.Join(t.TempDir(), "missing compiler")}})
	_, err := p.cmakeConfigureArgs("windows")
	if err == nil || !strings.Contains(err.Error(), "missing compiler") || !strings.Contains(err.Error(), "CMAKE_C_COMPILER") {
		t.Fatalf("missing compiler error = %v", err)
	}
}

func TestMenuconfigCommandPreservesArguments(t *testing.T) {
	args := []string{"--config", "config files/project config"}
	k := (&KConfigEntry{}).SetMenuconfigCmd("C:/Program Files/Kconfig/menu.exe", args...)
	args[0] = "changed"
	got := k.MenuconfigArgs()
	if k.MenuconfigCmd() != "C:/Program Files/Kconfig/menu.exe" || !slices.Equal(got, []string{"--config", "config files/project config"}) {
		t.Fatalf("command = %q %q", k.MenuconfigCmd(), got)
	}
	got[0] = "changed"
	if k.MenuconfigArgs()[0] != "--config" {
		t.Fatal("getter exposes mutable arguments")
	}
}

func TestShellToolProcess(t *testing.T) {
	if os.Getenv("VMAKE_SHELL_TOOL_PROCESS") != "1" {
		return
	}
	index := slices.Index(os.Args, "--")
	if index < 0 || index+1 >= len(os.Args) {
		os.Exit(2)
	}
	if err := os.WriteFile(os.Args[index+1], []byte("CONFIG_OK=y\n"), 0644); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func TestMakeAndConfigurePreserveSpacedToolPaths(t *testing.T) {
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Skip(err)
	}
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	toolDir := "compiler space"
	if runtime.GOOS != "windows" {
		toolDir += " $value `command`"
	}
	installDir := filepath.Join(t.TempDir(), toolDir)
	binDir := filepath.Join(installDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	toolName := "arm-none-eabi-gcc"
	if runtime.GOOS == "windows" {
		toolName += ".exe"
	}
	toolPath := filepath.Join(binDir, toolName)
	data, err := os.ReadFile(program)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toolPath, data, 0755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	makefile := "all:\n\t$(CC) -test.run=TestShellToolProcess -- cc.out\n\t$(CROSS_COMPILE)gcc -test.run=TestShellToolProcess -- prefix.out\nclean:\n\t$(CC) -test.run=TestShellToolProcess -- clean.out\ndefconfig:\n\t$(CC) -test.run=TestShellToolProcess -- .config\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(makefile), 0644); err != nil {
		t.Fatal(err)
	}
	configure := "#!/bin/sh\neval \"$CC -test.run=TestShellToolProcess -- configure.out\"\n"
	if err := os.WriteFile(filepath.Join(dir, "configure"), []byte(configure), 0755); err != nil {
		t.Fatal(err)
	}
	p := NewPackage().SetToolchain(&toolchain.Toolchain{
		TargetOS: "none", TargetTriple: "arm-none-eabi", Prefix: "arm-none-eabi-", InstallPath: installDir,
		Tools: toolchain.Tools{CC: toolName, MAKE: makePath},
	}).SetDirs(PkgDirs{SourceDir: dir, BuildDir: dir, InstallDir: filepath.Join(dir, "install")})
	p.AddKConfig("firmware").SelectPreset("defconfig")
	t.Setenv("VMAKE_SHELL_TOOL_PROCESS", "1")
	if err := p.Make(); err != nil {
		t.Fatal(err)
	}
	if err := NewCleanContext("firmware", nil).SetPackage(p).Make("clean"); err != nil {
		t.Fatal(err)
	}
	if !p.EnsureConfig(dir) {
		t.Fatal("preset was not generated")
	}
	if err := p.Configure(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cc.out", "prefix.out", "clean.out", ".config", "configure.out"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(data) != "CONFIG_OK=y\n" {
			t.Errorf("%s = %q, %v", name, data, err)
		}
	}
	if p.Env()["CC"] != filepath.ToSlash(toolPath) {
		t.Errorf("raw CC environment changed: %q", p.Env()["CC"])
	}
}
