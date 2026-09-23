package toolchain

import (
	"errors"
	"slices"
	"testing"
)

func TestRegistrationPublishesTogetherAndCopiesTools(t *testing.T) {
	m := &Manager{}
	r := NewRegistration(m)
	tc := &Toolchain{Name: "plugin", Tools: Tools{CC: "original"}}
	if err := r.RegisterToolchain("plugin", tc); err != nil {
		t.Fatal(err)
	}
	tc.Tools.CC = "changed"
	r.GetToolchains()["plugin"].Tools.CC = "also changed"
	r.AddGlobalCFlags("first", "repeat", "repeat")
	r.AddGlobalCxxFlags("cxx")
	r.AddGlobalLdFlags("ld")
	r.SetOnMissing("plugin", func(string) (*Toolchain, error) { return nil, errors.New("missing") })
	if len(m.extensions) != 0 || len(m.onMissing) != 0 || len(m.GetGlobalCFlags()) != 0 {
		t.Fatal("registration became visible before commit")
	}
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	if m.extensions["plugin"].Tools.CC != "original" || m.onMissing["plugin"] == nil {
		t.Fatal("toolchain snapshot or missing handler was not published")
	}
	if !slices.Equal(m.GetGlobalCFlags(), []string{"first", "repeat", "repeat"}) || !slices.Equal(m.GetGlobalCxxFlags(), []string{"cxx"}) || !slices.Equal(m.GetGlobalLdFlags(), []string{"ld"}) {
		t.Fatal("flag ordering changed")
	}
	r.GetToolchains()["plugin"].Tools.CC = "after commit"
	if m.extensions["plugin"].Tools.CC != "original" {
		t.Fatal("toolchain listing exposes manager state")
	}
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	r.AddGlobalCFlags("late")
	if !slices.Equal(m.GetGlobalCFlags(), []string{"first", "repeat", "repeat", "late"}) {
		t.Fatal("late committed flag callback was lost")
	}
}

func TestRegistrationAbortDiscardsEveryMutation(t *testing.T) {
	m := &Manager{}
	r := NewRegistration(m)
	if err := r.RegisterToolchain("plugin", &Toolchain{Name: "plugin"}); err != nil {
		t.Fatal(err)
	}
	r.SetOnMissing("plugin", func(string) (*Toolchain, error) { return nil, nil })
	r.AddGlobalCFlags("failed")
	r.Abort()
	r.AddGlobalCFlags("late")
	r.SetOnMissing("other", func(string) (*Toolchain, error) { return nil, nil })
	if err := r.Commit(); err == nil {
		t.Fatal("aborted registration committed")
	}
	if len(m.extensions) != 0 || len(m.onMissing) != 0 || len(m.GetGlobalCFlags()) != 0 {
		t.Fatal("aborted plugin left registered state")
	}
}

func TestRegistrationConflictDoesNotPartiallyCommit(t *testing.T) {
	m := &Manager{}
	r := NewRegistration(m)
	for _, name := range []string{"new", "conflict"} {
		if err := r.RegisterToolchain(name, &Toolchain{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	r.AddGlobalLdFlags("failed")
	r.SetOnMissing("new", func(string) (*Toolchain, error) { return nil, nil })
	if err := m.RegisterToolchain("conflict", &Toolchain{Name: "conflict"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(); err == nil {
		t.Fatal("conflicting registration succeeded")
	}
	if len(m.extensions) != 1 || m.extensions["new"] != nil || len(m.onMissing) != 0 || len(m.GetGlobalLdFlags()) != 0 {
		t.Fatal("conflicting registration partially committed")
	}
}

func TestRegistrationPostCommitSnapshotsToolchains(t *testing.T) {
	m := &Manager{}
	r := NewRegistration(m)
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	tc := &Toolchain{Name: "plugin", Tools: Tools{CC: "original"}}
	if err := r.RegisterToolchain("plugin", tc); err != nil {
		t.Fatal(err)
	}
	tc.Tools.CC = "changed"
	if m.extensions["plugin"].Tools.CC != "original" {
		t.Fatal("post-commit registration shares the caller toolchain")
	}
	if r.RegisterToolchain("plugin", nil) == nil {
		t.Fatal("post-commit registration accepted a nil toolchain")
	}
}

func TestRegistrationNativeSymbolsAreNotExposed(t *testing.T) {
	for _, name := range []string{"Registration", "NewRegistration"} {
		if _, exists := YaegiSymbols()[name]; exists {
			t.Fatalf("native symbol %s is exposed to scripts", name)
		}
	}
}
