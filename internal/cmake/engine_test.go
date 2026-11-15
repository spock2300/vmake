package cmake

import "testing"

// A configure preset owns its generator and build directory, so the plan runs
// in the source directory and does not impose the host default generator.
func TestEnginePlanConfigurePresetRunsInSourceDir(t *testing.T) {
	s := newIncrementalSettings(t)
	s.HostOS = "windows"
	plan, err := NewEngine(s).PlanConfigure([]string{"--preset", "release"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Preset {
		t.Fatal("Plan.Preset = false")
	}
	if plan.Dir != s.SourceDir {
		t.Fatalf("Dir = %q, want the source dir %q", plan.Dir, s.SourceDir)
	}
	if plan.Executable == "" {
		t.Fatal("plan lost the resolved executable")
	}
	if HasOption(plan.Args, "-G") {
		t.Fatalf("preset plan injected a generator: %v", plan.Args)
	}
}

func TestEnginePlanConfigureUsesBuildRootWithoutPreset(t *testing.T) {
	s := newIncrementalSettings(t)
	s.HostOS = "windows"
	plan, err := NewEngine(s).PlanConfigure(nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Preset {
		t.Fatal("Plan.Preset = true for a plain configure")
	}
	if plan.Dir != s.BuildRoot {
		t.Fatalf("Dir = %q, want the build root %q", plan.Dir, s.BuildRoot)
	}
	if !HasOption(plan.Args, "-G", "Ninja") {
		t.Fatalf("windows plain configure does not pin Ninja: %v", plan.Args)
	}
}
