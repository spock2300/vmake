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
	first.AddCommand(dir, "shared.c", "one.o", &CompileOptions{CFlags: []string{"-DFIRST=1"}})
	if err := first.Save(output); err != nil {
		t.Fatal(err)
	}
	second := NewCompileCommandsWriter(tools)
	second.AddCommand(dir, "shared.c", "two.o", &CompileOptions{})
	if err := second.Save(output); err != nil {
		t.Fatal(err)
	}
	first.AddCommand(dir, "shared.c", "one.o", &CompileOptions{CFlags: []string{"-DFIRST=2"}})
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
