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
	if runtime.GOOS == "darwin" {
		t.Skip("Apple as has no dependency-file option; .S header deps are covered by the darwin clang tests")
	}
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
					record := filepath.Join(dir, "compile.json")
					inputs := append([]string{source}, deps...)
					if err := saveActionRecord(record, "assembly", dir, inputs, []string{obj}); err != nil {
						t.Fatal(err)
					}
					if !actionUpToDate(record, "assembly", dir, inputs, []string{obj}) {
						t.Fatal("unchanged object is stale")
					}
					for _, dep := range want {
						path := filepath.Join(dir, dep)
						data, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(path, append(data, []byte("\n")...), 0644); err != nil {
							t.Fatal(err)
						}
						if actionUpToDate(record, "assembly", dir, inputs, []string{obj}) {
							t.Fatalf("edit to %s did not invalidate object", dep)
						}
						if err := os.Remove(path); err != nil {
							t.Fatal(err)
						}
						if actionUpToDate(record, "assembly", dir, inputs, []string{obj}) {
							t.Fatalf("deletion of %s did not invalidate object", dep)
						}
						if err := os.WriteFile(path, data, 0644); err != nil {
							t.Fatal(err)
						}
						if _, err := compiler.Compile(source, obj, opts, dir); err != nil {
							t.Fatal(err)
						}
						if err := saveActionRecord(record, "assembly", dir, inputs, []string{obj}); err != nil {
							t.Fatal(err)
						}
						if !actionUpToDate(record, "assembly", dir, inputs, []string{obj}) {
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
			obj, err := objectPath("p:assembly", source, dir)
			if err != nil {
				t.Error(err)
				return
			}
			_, err = compiler.Compile(source, obj, &CompileOptions{Language: "asm-cpp", Includes: []string{"include"}}, dir)
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	for index, source := range sources {
		rel, err := objectPath("p:assembly", source, dir)
		if err != nil {
			t.Fatal(err)
		}
		obj := filepath.Join(dir, rel)
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

var machoObjectHeader = []byte{
	0xcf, 0xfa, 0xed, 0xfe,
	0x0c, 0x00, 0x00, 0x01,
	0x00, 0x00, 0x00, 0x00,
	0x01, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00,
}

func darwinClangAssembly(t *testing.T, commands *[][]string, writePP bool) *Compiler {
	t.Helper()
	return &Compiler{
		ccPath:   "cc",
		clangCC:  true,
		targetOS: "darwin",
		run: func(name, workDir string, args ...string) ([]byte, error) {
			*commands = append(*commands, append([]string{name}, args...))
			for i, arg := range args {
				if arg != "-MF" || i+1 >= len(args) {
					continue
				}
				dep := args[i+1]
				if strings.HasSuffix(dep, ".as") {
					t.Errorf("darwin assembly must not request assembler deps: %v", args)
				}
				if writePP && strings.HasSuffix(dep, ".pp") {
					if err := os.WriteFile(resolveWorkPath(workDir, dep), []byte("object.o: source.S header.h\n"), 0644); err != nil {
						return nil, err
					}
				}
			}
			return nil, os.WriteFile(filepath.Join(workDir, "object.o"), machoObjectHeader, 0644)
		},
	}
}

func TestClangAssemblyDarwinSkipsAssemblerDeps(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "source.s", ".include \"extra.inc\"\n.byte 1\n")
	writeAssemblyFixture(t, dir, "extra.inc", ".byte 2\n")

	var commands [][]string
	compiler := darwinClangAssembly(t, &commands, false)
	deps, err := compiler.Compile("source.s", "object.o", &CompileOptions{Language: "asm"}, dir)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(deps) != 0 {
		t.Fatalf("deps = %v, want none where the assembler has no dep-file option", deps)
	}
	if len(commands) != 1 {
		t.Fatalf("commands = %v, want a single assembly step", commands)
	}
	args := commands[0]
	for _, flag := range []string{"--MD", "-MMD", "-MP", "-MF"} {
		if slices.Contains(args, flag) {
			t.Fatalf("args = %v, darwin clang assembly must skip assembler dependency flags", args)
		}
	}
	if !slices.Contains(args, "-fno-integrated-as") {
		t.Fatalf("args = %v, want -fno-integrated-as", args)
	}
	if _, err := os.Stat(filepath.Join(dir, "object.o.d")); err != nil {
		t.Fatalf("dep file: %v", err)
	}
}

func TestClangAssemblyDarwinKeepsPreprocessorDeps(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "source.S", "#include \"header.h\"\n.byte 1\n")
	writeAssemblyFixture(t, dir, "header.h", "#define VALUE 1\n")

	var commands [][]string
	compiler := darwinClangAssembly(t, &commands, true)
	deps, err := compiler.Compile("source.S", "object.o", &CompileOptions{Language: "asm-cpp"}, dir)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !slices.Contains(deps, "header.h") {
		t.Fatalf("deps = %v, want preprocessor header header.h", deps)
	}
	assemble := commands[len(commands)-1]
	for _, flag := range []string{"--MD", "-Xassembler"} {
		if slices.Contains(assemble, flag) {
			t.Fatalf("assemble args = %v, darwin must not use assembler dependency flags", assemble)
		}
	}
}
