package build

import (
	"bytes"
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func assemblyTestTools(t *testing.T, name string) *ResolvedTools {
	t.Helper()
	if name == "clang" && runtime.GOOS != "linux" {
		t.Skip("Clang GNU assembler integration requires a Linux host")
	}
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s unavailable", name)
	}
	version, err := compilerVersion(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &ResolvedTools{CC: path, CCVersion: version, targetOS: runtime.GOOS}
}

func writeAssemblyFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestAssemblyCompilerDependencies(t *testing.T) {
	for _, compilerName := range []string{"gcc", "clang"} {
		t.Run(compilerName, func(t *testing.T) {
			tools := assemblyTestTools(t, compilerName)
			for _, extension := range []string{".s", ".S"} {
				t.Run(extension, func(t *testing.T) {
					dir := filepath.Join(t.TempDir(), "source space")
					source := "source" + extension
					text := ".include \"asm.inc\"\n"
					if extension == ".S" {
						text += "#include \"pre.h\"\n.byte VALUE + EXTRA\n"
					}
					writeAssemblyFixture(t, dir, source, text)
					writeAssemblyFixture(t, dir, "include space/asm.inc", ".include \"nested.inc\"\n.byte 7\n")
					writeAssemblyFixture(t, dir, "include space/nested.inc", ".byte 3\n")
					writeAssemblyFixture(t, dir, "include space/pre.h", "#define VALUE 5\n")
					obj := filepath.Join("objects space", "different-name.o")
					opts := &CompileOptions{
						Language: sourceLanguage(source), Includes: []string{"include space"}, Defines: []string{"EXTRA=1"},
						CFlags: []string{"-Wall", "-Wextra", "-Werror", "-Wstrict-prototypes", "-O2", "-fPIC", "-DUNUSED=1"},
					}
					compiler := NewCompiler(tools)
					deps, err := compiler.Compile(source, obj, opts, dir)
					if err != nil {
						t.Fatal(err)
					}
					want := []string{"include space/asm.inc", "include space/nested.inc"}
					if extension == ".S" {
						want = append(want, "include space/pre.h")
					}
					for _, dep := range want {
						if !slices.ContainsFunc(deps, func(value string) bool { return filepath.ToSlash(value) == dep }) {
							t.Fatalf("missing dependency %q in %v", dep, deps)
						}
					}
					if len(deps) != len(want) {
						t.Fatalf("unexpected dependencies: %v", deps)
					}
					if valid, _ := IsSourceValid(source, obj, dir); !valid {
						t.Fatal("unchanged object is stale")
					}
					for _, dep := range want {
						path := filepath.Join(dir, dep)
						data, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						future := time.Now().Add(time.Minute)
						if err := os.Chtimes(path, future, future); err != nil {
							t.Fatal(err)
						}
						if valid, _ := IsSourceValid(source, obj, dir); valid {
							t.Fatalf("edit to %s did not invalidate object", dep)
						}
						if err := os.Remove(path); err != nil {
							t.Fatal(err)
						}
						if valid, _ := IsSourceValid(source, obj, dir); valid {
							t.Fatalf("deletion of %s did not invalidate object", dep)
						}
						if err := os.WriteFile(path, data, 0644); err != nil {
							t.Fatal(err)
						}
						if _, err := compiler.Compile(source, obj, opts, dir); err != nil {
							t.Fatal(err)
						}
						if valid, _ := IsSourceValid(source, obj, dir); !valid {
							t.Fatalf("recompiled object is stale after restoring %s", dep)
						}
					}
					if tools.isClangCC() {
						for _, suffix := range []string{".d.pp", ".d.as"} {
							if _, err := os.Stat(filepath.Join(dir, obj+suffix)); !os.IsNotExist(err) {
								t.Fatalf("intermediate depfile remains: %s (%v)", suffix, err)
							}
						}
					}
				})
			}
		})
	}
}

func TestClangAssemblyParallelSourceNames(t *testing.T) {
	compiler := NewCompiler(assemblyTestTools(t, "clang"))
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "include/asm.inc", ".byte 7\n")
	sources := []string{"first/source.S", "second/source.S"}
	for index, source := range sources {
		writeAssemblyFixture(t, dir, source, ".include \"asm.inc\"\n.byte "+string(rune('1'+index))+"\n")
	}
	var wg sync.WaitGroup
	for _, source := range sources {
		wg.Go(func() {
			obj := filepath.Join("objects", objectName(source))
			_, err := compiler.Compile(source, obj, &CompileOptions{Language: "asm-cpp", Includes: []string{"include"}}, dir)
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	for index, source := range sources {
		obj := filepath.Join(dir, "objects", objectName(source))
		file, err := elf.Open(obj)
		if err != nil {
			t.Fatal(err)
		}
		data, err := file.Section(".text").Data()
		file.Close()
		if err != nil || !bytes.Equal(data, []byte{7, byte(index + 1)}) {
			t.Fatalf("%s: data=%v err=%v", source, data, err)
		}
	}
}

func TestClangAssemblyFailureCleansOutputs(t *testing.T) {
	tools := assemblyTestTools(t, "clang")
	for _, failure := range []string{"assembler", "wrong-format"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			writeAssemblyFixture(t, dir, "source.S", ".byte 1\n")
			compiler := NewCompiler(tools)
			opts := &CompileOptions{Language: "asm-cpp"}
			if _, err := compiler.Compile("source.S", "object.o", opts, dir); err != nil {
				t.Fatal(err)
			}
			if failure == "assembler" {
				writeAssemblyFixture(t, dir, "source.S", ".invalid_assembly_directive\n")
			} else {
				compiler.targetOS = "windows"
			}
			_, err := compiler.Compile("source.S", "object.o", opts, dir)
			if err == nil || failure == "wrong-format" && !strings.Contains(err.Error(), "expected COFF object") {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, suffix := range []string{"", ".s", ".d", ".d.pp", ".d.as"} {
				if _, err := os.Stat(filepath.Join(dir, "object.o"+suffix)); !os.IsNotExist(err) {
					t.Fatalf("failed compilation left object.o%s: %v", suffix, err)
				}
			}
		})
	}
}

func TestClangAssemblyObjectFormats(t *testing.T) {
	tools := assemblyTestTools(t, "clang")
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "source.s", ".byte 1\n")
	cmd := exec.Command(tools.CC, "--target=x86_64-w64-mingw32", "-c", "source.s", "-o", "coff.o")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Clang COFF fixture: %s: %v", output, err)
	}
	path := filepath.Join(dir, "coff.o")
	if err := validateAssemblyObject(path, "windows"); err != nil {
		t.Fatal(err)
	}
	if err := validateAssemblyObject(path, "linux"); err == nil {
		t.Fatal("accepted COFF object for ELF target")
	}
}

func TestClangAssemblyCompileCommand(t *testing.T) {
	tools := &ResolvedTools{CC: "renamed-compiler", CCVersion: "vendor clang version 17.0.6", targetOS: "linux"}
	writer := NewCompileCommandsWriter(tools)
	opts := &CompileOptions{Language: "asm-cpp", CFlags: []string{"--target=x86_64-linux-gnu", "-B/toolchain/bin", "-fintegrated-as"}}
	writer.AddCommand(t.TempDir(), "source.S", "objects/source.S.o", opts)
	args := writer.commands[0].Arguments
	for _, want := range []string{"--target=x86_64-linux-gnu", "-B/toolchain/bin", "-fno-integrated-as", "source.S"} {
		if !slices.Contains(args, want) {
			t.Fatalf("missing %q in %v", want, args)
		}
	}
	if slices.Index(args, "-fno-integrated-as") < slices.Index(args, "-fintegrated-as") || slices.Contains(args, "-save-temps=obj") {
		t.Fatalf("incorrect Clang assembly command: %v", args)
	}
	if opts.clang {
		t.Fatal("compile command mutated caller options")
	}
}
