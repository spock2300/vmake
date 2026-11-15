package build

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/jsonio"
)

func TestCompileCommandsMergePreservesTargetOutputs(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "compile_commands.json")
	tools := &ResolvedTools{CC: "cc", CXX: "c++"}
	first := NewCompileCommandsWriter(tools)
	first.AddSpec(dir, "shared.c", plannedCCSpec("one.o", "shared.c", "-DFIRST=1"))
	if err := first.Save(output); err != nil {
		t.Fatal(err)
	}
	second := NewCompileCommandsWriter(tools)
	second.AddSpec(dir, "shared.c", plannedCCSpec("two.o", "shared.c"))
	if err := second.Save(output); err != nil {
		t.Fatal(err)
	}
	first.AddSpec(dir, "shared.c", plannedCCSpec("one.o", "shared.c", "-DFIRST=2"))
	if err := first.Save(output); err != nil {
		t.Fatal(err)
	}
	var commands []CompileCommand
	if err := jsonio.Load(output, &commands); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 {
		t.Fatalf("got %d commands, want distinct one.o and two.o", len(commands))
	}
	for _, command := range commands {
		if strings.Contains(command.Command, "-DFIRST=1") {
			t.Fatalf("stale command survived: %s", command.Command)
		}
	}
}

// plannedCCSpec is a compile command in the shape planCompile produces; these
// tests exercise the writer, not command derivation.
func plannedCCSpec(objPath, src string, extra ...string) commandSpec {
	args := []string{"-c", "-o", objPath}
	args = append(args, extra...)
	args = append(args, src)
	return commandSpec{Program: "cc", Args: args}
}

func TestCompileCommandsAddSpecUsesPlannedCommand(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "compile_commands.json")
	writer := NewCompileCommandsWriter(&ResolvedTools{CC: "cc", CXX: "c++"})
	writer.AddSpec(dir, "start.S", commandSpec{Program: "clang", Args: []string{"-c", "-x", "assembler-with-cpp", "-o", "obj/start.o", "start.S"}})
	if err := writer.Save(output); err != nil {
		t.Fatal(err)
	}
	var commands []CompileCommand
	if err := jsonio.Load(output, &commands); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(commands))
	}
	got := strings.Join(commands[0].Arguments, " ")
	if !strings.HasPrefix(got, "clang -c -x assembler-with-cpp -o obj/start.o") {
		t.Fatalf("arguments = %q", got)
	}
}
