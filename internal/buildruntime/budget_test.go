package buildruntime

import (
	"slices"
	"testing"
)

func TestMakeBudget(t *testing.T) {
	budget := NewBudget(4)
	for _, test := range []struct {
		name string
		args []string
		env  map[string]string
		want []string
	}{
		{"default", []string{"all"}, nil, []string{"-j4", "all"}},
		{"explicit short", []string{"-j", "2", "all"}, nil, []string{"-j", "2", "all"}},
		{"explicit attached", []string{"-j2", "all"}, nil, []string{"-j2", "all"}},
		{"explicit long", []string{"--jobs=3", "all"}, nil, []string{"--jobs=3", "all"}},
		{"makeflags", []string{"all"}, map[string]string{"MAKEFLAGS": "-j2"}, []string{"all"}},
		{"mflags", []string{"all"}, map[string]string{"MFLAGS": "-j3"}, []string{"all"}},
		{"gnumakeflags", []string{"all"}, map[string]string{"GNUMAKEFLAGS": "--jobs=2"}, []string{"all"}},
		{"after double dash", []string{"--", "-j2"}, nil, []string{"-j4", "--", "-j2"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := slices.Clone(test.args)
			got, err := budget.MakeArgs(test.args, test.env)
			if err != nil || !slices.Equal(got, test.want) {
				t.Fatalf("MakeArgs(%q) = %q, %v; want %q", test.args, got, err, test.want)
			}
			if !slices.Equal(test.args, before) {
				t.Fatal("mutated caller arguments")
			}
		})
	}
}

func TestCMakeBudget(t *testing.T) {
	budget := NewBudget(4)
	for _, test := range []struct {
		name string
		args []string
		env  map[string]string
		want []string
	}{
		{"default", nil, nil, []string{"--parallel", "4"}},
		{"with target", []string{"--target", "app"}, nil, []string{"--target", "app", "--parallel", "4"}},
		{"explicit j", []string{"--target", "app", "-j2"}, nil, []string{"--target", "app", "-j2"}},
		{"explicit parallel", []string{"--parallel", "2"}, nil, []string{"--parallel", "2"}},
		{"explicit parallel eq", []string{"--parallel=2"}, nil, []string{"--parallel=2"}},
		{"env override", nil, map[string]string{"CMAKE_BUILD_PARALLEL_LEVEL": "3"}, nil},
		{"before backend", []string{"--target", "app", "--", "-v"}, nil, []string{"--target", "app", "--parallel", "4", "--", "-v"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := budget.CMakeBuildArgs(test.args, test.env)
			if err != nil || !slices.Equal(got, test.want) {
				t.Fatalf("CMakeBuildArgs(%q) = %q, %v; want %q", test.args, got, err, test.want)
			}
		})
	}
	if got, err := budget.CMakeInstallArgs([]string{"--component", "Runtime"}, nil); err != nil || !slices.Equal(got, []string{"--component", "Runtime"}) {
		t.Fatalf("CMakeInstallArgs = %q, %v", got, err)
	}
}
