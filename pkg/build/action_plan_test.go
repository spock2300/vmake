package build

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPlannedActionSignatureCoversSchema(t *testing.T) {
	base := &plannedAction{
		Kind:            "link",
		Target:          "app:app",
		WorkDir:         "/work",
		TargetOS:        "linux",
		Tools:           "cc@1",
		Environment:     "env",
		Commands:        []commandSpec{{Program: "cc", Args: []string{"-o", "app"}}},
		Inputs:          []string{"a.o"},
		SignatureInputs: []string{"a.o"},
		Outputs:         []string{"app"},
		RecordPath:      "/work/app.vmake.json",
	}
	signature := func(t *testing.T, action *plannedAction) string {
		t.Helper()
		got, err := action.signature()
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	baseSignature := signature(t, base)
	mutations := map[string]func(*plannedAction){
		"kind":             func(a *plannedAction) { a.Kind = "compile" },
		"target":           func(a *plannedAction) { a.Target = "app:other" },
		"workdir":          func(a *plannedAction) { a.WorkDir = "/other" },
		"target os":        func(a *plannedAction) { a.TargetOS = "windows" },
		"tools":            func(a *plannedAction) { a.Tools = "cc@2" },
		"environment":      func(a *plannedAction) { a.Environment = "env2" },
		"commands":         func(a *plannedAction) { a.Commands[0].Args = append(a.Commands[0].Args, "-g") },
		"signature inputs": func(a *plannedAction) { a.SignatureInputs = append(a.SignatureInputs, "b.o") },
		"outputs":          func(a *plannedAction) { a.Outputs = append(a.Outputs, "app.map") },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			copy := *base
			copy.Commands = append([]commandSpec{}, base.Commands...)
			copy.Commands[0].Args = append([]string{}, base.Commands[0].Args...)
			copy.SignatureInputs = append([]string{}, base.SignatureInputs...)
			copy.Outputs = append([]string{}, base.Outputs...)
			mutate(&copy)
			if got := signature(t, &copy); got == baseSignature {
				t.Fatalf("signature did not change after mutating %s", name)
			}
		})
	}
	copy := *base
	copy.RecordPath = "/work/other.json"
	if got := signature(t, &copy); got != baseSignature {
		t.Fatal("record path must not be part of the action signature")
	}
}

func compilePlanTestCompiler(t *testing.T, version string) *Compiler {
	t.Helper()
	return NewCompiler(&ResolvedTools{CC: "/usr/bin/cc", CXX: "/usr/bin/c++", CCVersion: version, targetOS: "linux"})
}

func writeFakeDepfile(t *testing.T, path, source string, headers ...string) {
	t.Helper()
	content := source + ": " + strings.Join(append([]string{source}, headers...), " ") + "\n"
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestPlanCompileCommandsAreHashedAndExecuted(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.c")
	if err := os.WriteFile(src, []byte("int main(void){return 0;}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	compiler := compilePlanTestCompiler(t, "gcc (test) 12.0")
	opts := &CompileOptions{Language: "c", Includes: []string{"include"}, Defines: []string{"X=1"}, CFlags: []string{"-Wall"}}
	plan, err := compiler.planCompile(src, "obj/main.o", opts, dir)
	if err != nil {
		t.Fatal(err)
	}
	var executed [][]string
	compiler.run = func(name, workDir string, args ...string) ([]byte, error) {
		executed = append(executed, append([]string{name}, args...))
		writeFakeDepfile(t, filepath.Join(dir, plan.DepPath), src, "include/header.h")
		return nil, nil
	}
	deps, err := compiler.executeCompilePlan(t.Context(), plan, src, "obj/main.o", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(executed) != len(plan.Commands) {
		t.Fatalf("executed %d commands, plan has %d", len(executed), len(plan.Commands))
	}
	for i, command := range plan.Commands {
		want := append([]string{command.Program}, command.Args...)
		if !reflect.DeepEqual(executed[i], want) {
			t.Fatalf("command %d = %v, want %v", i, executed[i], want)
		}
	}
	if !reflect.DeepEqual(deps, []string{"include/header.h"}) {
		t.Fatalf("deps = %v", deps)
	}
	if plan.Display.Program != plan.Commands[0].Program {
		t.Fatalf("display program = %q, want %q", plan.Display.Program, plan.Commands[0].Program)
	}
}

func TestPlanCompileClangAssemblyCommandsArePlanned(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "start.S")
	if err := os.WriteFile(src, []byte(".byte 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	compiler := compilePlanTestCompiler(t, "clang version 17.0.0")
	opts := &CompileOptions{Language: "asm-cpp"}
	plan, err := compiler.planCompile(src, "obj/start.o", opts, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Commands) != 2 {
		t.Fatalf("clang assembly plan commands = %d, want 2", len(plan.Commands))
	}
	if !strings.Contains(strings.Join(plan.Display.Args, " "), "assembler-with-cpp") {
		t.Fatalf("clang assembly display lost preprocessing: %v", plan.Display.Args)
	}
	action := &plannedAction{
		Kind: "compile", Target: "app:start", WorkDir: dir, TargetOS: "linux",
		Tools: "clang@17", Environment: "env", Commands: plan.Commands,
		Outputs: plan.Outputs, RecordPath: filepath.Join(dir, "start.vmake.json"),
	}
	before, err := action.signature()
	if err != nil {
		t.Fatal(err)
	}
	action.Commands[0].Args = append(action.Commands[0].Args, "-DTEST=1")
	after, err := action.signature()
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("preprocessor command change did not rotate the action signature")
	}
}

func TestRunPlannedActionRecordsPostRunInputs(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.c")
	if err := os.WriteFile(src, []byte("int main(void){return 0;}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	newHeader := filepath.Join(dir, "new.h")
	if err := os.WriteFile(newHeader, []byte("#define NEW 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "main.o")
	if err := os.WriteFile(output, []byte("object"), 0644); err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(dir, "main.o.vmake.json")
	s := &Scheduler{hashes: newFileHashCache()}
	action := &plannedAction{
		Kind:         "compile",
		Target:       "app:main",
		WorkDir:      dir,
		Commands:     []commandSpec{{Program: "cc", Args: []string{"-c", src}}},
		Inputs:       []string{src, filepath.Join(dir, "deleted.h")},
		RecordInputs: []string{src},
		Outputs:      []string{output},
		RecordPath:   recordPath,
	}
	upToDate, _, err := s.runPlannedAction(t.Context(), action, func(context.Context) ([]string, error) {
		return []string{newHeader}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if upToDate {
		t.Fatal("first action unexpectedly up to date")
	}
	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	var record actionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	got := make(map[string]bool, len(record.Inputs))
	for _, input := range record.Inputs {
		got[filepath.Base(input.Path)] = true
	}
	if !got[filepath.Base(src)] || !got[filepath.Base(newHeader)] {
		t.Fatalf("record inputs = %v, want source and new header", got)
	}
	if got["deleted.h"] {
		t.Fatalf("record retained a pre-run dependency: %v", got)
	}
}
