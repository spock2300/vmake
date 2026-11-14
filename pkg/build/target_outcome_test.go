package build

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestSessionRecordsTargetOutcome(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "main.c", "int main(void) { return 0; }\n")
	target := makeTargetWithDeps("app").SetKind(api.TargetBinary).AddFiles("main.c")
	session := NewSession(nil)

	first := nativeTestSchedulerWithSession(t, dir, session, target)
	if err := first.BuildAll(); err != nil {
		t.Fatal(err)
	}

	results := session.TargetResults()
	if len(results) != 1 {
		t.Fatalf("target results = %+v", results)
	}
	result := results[0]
	if result.Package != "p" || result.Target != "app" || result.Status != TargetStatusBuilt {
		t.Fatalf("target result = %+v", result)
	}
	if len(result.Outputs) == 0 {
		t.Fatal("target result has no outputs")
	}
	placement, ok := session.TargetPlacements()["p:app"]
	if !ok || placement.OutputPath == "" || placement.InstallName == "" {
		t.Fatalf("placement = %+v, ok = %v", placement, ok)
	}

	secondSession := NewSession(nil)
	second := nativeTestSchedulerWithSession(t, dir, secondSession, target)
	if err := second.BuildAll(); err != nil {
		t.Fatal(err)
	}
	results = secondSession.TargetResults()
	if len(results) != 1 || results[0].Status != TargetStatusUpToDate {
		t.Fatalf("rebuilt target results = %+v", results)
	}
	if _, ok := secondSession.TargetPlacements()["p:app"]; !ok {
		t.Fatal("up-to-date target lost its placement")
	}
}

func TestSubGraphTargetOutcomeUsesSharedSession(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "lib.c", "int lib(void) { return 1; }\n")
	target := makeTargetWithDeps("lib").SetKind(api.TargetStatic).AddFiles("lib.c")
	source := nativeTestScheduler(t, dir, target)
	session := NewSession(nil)
	dirs := map[string]*api.PkgDirs{
		"p": {SourceDir: dir, BuildDir: filepath.Join(dir, "build")},
	}
	env := testBuildEnv(dirs, testBuildPackages(dirs), map[string]*toolchain.Toolchain{"p": source.toolchain}, session, dir)
	base := testBuildJob(nil, source.toolchain, env, api.ModeDebug, api.Platform{OS: runtime.GOOS}, false)
	req := SubGraphRequest{
		RootPkg: "p",
		Targets: map[string]map[string]*api.Target{"p": {"lib": target}},
		PkgMeta: map[string]PkgBuildMeta{"p": {}},
		Needed:  map[string]bool{"p": true},
	}
	if err := BuildSubGraph(base, req); err != nil {
		t.Fatal(err)
	}

	placement, ok := session.TargetPlacements()["p:lib"]
	if !ok || placement.OutputPath == "" {
		t.Fatalf("subgraph placement = %+v, ok = %v", placement, ok)
	}
	results := session.TargetResults()
	if len(results) != 1 || results[0].Status != TargetStatusBuilt {
		t.Fatalf("subgraph target results = %+v", results)
	}
}

func TestFailedTargetDoesNotRecordOutcome(t *testing.T) {
	dir := t.TempDir()
	target := makeTargetWithDeps("app").SetKind(api.TargetBinary).AddFiles("missing.c")
	session := NewSession(nil)
	s := nativeTestSchedulerWithSession(t, dir, session, target)
	if err := s.BuildAll(); err == nil {
		t.Fatal("expected missing source build to fail")
	}
	if results := session.TargetResults(); len(results) != 0 {
		t.Fatalf("failed target recorded results: %+v", results)
	}
	if placements := session.TargetPlacements(); len(placements) != 0 {
		t.Fatalf("failed target recorded placements: %+v", placements)
	}
}

func TestIncrementalVoidOutcomeIsAlwaysBuilt(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.txt")
	output := filepath.Join(dir, "output.txt")
	if err := os.WriteFile(input, []byte("input\n"), 0644); err != nil {
		t.Fatal(err)
	}
	target := makeTargetWithDeps("callback").
		SetKind(api.TargetVoid).
		AddInputs(input).
		AddOutputs(output).
		SetBuildFunc(func(*api.Package) error {
			return os.WriteFile(output, []byte("output\n"), 0644)
		})

	firstSession := NewSession(nil)
	first := nativeTestSchedulerWithSession(t, dir, firstSession, target)
	if err := first.BuildAll(); err != nil {
		t.Fatal(err)
	}
	if results := firstSession.TargetResults(); len(results) != 1 || results[0].Status != TargetStatusBuilt {
		t.Fatalf("first void result = %+v", results)
	}
	if placements := firstSession.TargetPlacements(); len(placements) != 0 {
		t.Fatalf("void target exposed placements: %+v", placements)
	}

	secondSession := NewSession(nil)
	second := nativeTestSchedulerWithSession(t, dir, secondSession, target)
	if err := second.BuildAll(); err != nil {
		t.Fatal(err)
	}
	if results := secondSession.TargetResults(); len(results) != 1 || results[0].Status != TargetStatusBuilt {
		t.Fatalf("up-to-date void result = %+v", results)
	}
}

func TestTargetResultsAreDefensiveCopies(t *testing.T) {
	session := NewSession(nil)
	session.recordTarget(targetOutcome{
		FullName: "p:app",
		Result: TargetResult{
			Package: "p",
			Target:  "app",
			Kind:    string(api.TargetBinary),
			Outputs: []string{"first"},
			Status:  TargetStatusBuilt,
			Size:    &TargetSize{Total: 1},
		},
		Placement: ArtifactPlacement{OutputPath: "first", PostLinkOutputs: []string{"first.hex"}},
	})

	results := session.TargetResults()
	results[0].Outputs[0] = "mutated"
	results[0].Size.Total = 99
	if got := session.TargetResults()[0]; got.Outputs[0] != "first" || got.Size.Total != 1 {
		t.Fatalf("target result mutated through accessor: %+v", got)
	}
	placements := session.TargetPlacements()
	placements["p:app"].PostLinkOutputs[0] = "mutated.hex"
	if got := session.TargetPlacements()["p:app"]; got.PostLinkOutputs[0] != "first.hex" {
		t.Fatalf("placement mutated through accessor: %+v", got)
	}
}

func TestFailedCompileDoesNotRecordOutcome(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "bad.c", "int main(void) { return 0;\n")
	target := makeTargetWithDeps("app").SetKind(api.TargetBinary).AddFiles("bad.c")
	session := NewSession(nil)
	s := nativeTestSchedulerWithSession(t, dir, session, target)
	if err := s.BuildAll(); err == nil {
		t.Fatal("expected invalid source build to fail")
	}
	if results := session.TargetResults(); len(results) != 0 {
		t.Fatalf("failed compile recorded results: %+v", results)
	}
	if placements := session.TargetPlacements(); len(placements) != 0 {
		t.Fatalf("failed compile recorded placements: %+v", placements)
	}
}

func TestObjectTargetRecordsPlacementWithoutMainOutput(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "obj.c", "int object(void) { return 1; }\n")
	target := makeTargetWithDeps("obj").SetKind(api.TargetObject).AddFiles("obj.c")
	session := NewSession(nil)
	s := nativeTestSchedulerWithSession(t, dir, session, target)
	if err := s.BuildAll(); err != nil {
		t.Fatal(err)
	}
	results := session.TargetResults()
	if len(results) != 1 || len(results[0].Outputs) != 0 {
		t.Fatalf("object result = %+v", results)
	}
	placement, ok := session.TargetPlacements()["p:obj"]
	if !ok || placement.OutputPath == "" {
		t.Fatalf("object placement = %+v, ok = %v", placement, ok)
	}
}

func TestPartialBuildKeepsCompletedTargetOutcome(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "good.c", "int main(void) { return 0; }\n")
	writeAssemblyFixture(t, dir, "bad.c", "int main(void) { return 0;\n")
	good := makeTargetWithDeps("good").SetKind(api.TargetBinary).AddFiles("good.c")
	bad := makeTargetWithDeps("bad", "good").SetKind(api.TargetBinary).AddFiles("bad.c")
	session := NewSession(nil)
	s := nativeTestSchedulerWithSession(t, dir, session, good, bad)
	if err := s.BuildAll(); err == nil {
		t.Fatal("expected dependent target build to fail")
	}
	results := session.TargetResults()
	if len(results) != 1 || results[0].Target != "good" {
		t.Fatalf("partial target results = %+v", results)
	}
	if _, ok := session.TargetPlacements()["p:good"]; !ok {
		t.Fatal("completed target lost its placement")
	}
	if _, ok := session.TargetPlacements()["p:bad"]; ok {
		t.Fatal("failed target recorded a placement")
	}
}
