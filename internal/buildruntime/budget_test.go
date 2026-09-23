package buildruntime

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestMakeBudget(t *testing.T) {
	budget := Budget{Jobs: 4}
	for _, test := range []struct {
		name string
		args []string
		env  map[string]string
		want []string
	}{
		{"default", []string{"all"}, nil, []string{"-j4", "all"}},
		{"explicit", []string{"-j", "2", "all"}, nil, []string{"-j2", "all"}},
		{"long", []string{"--jobs=3", "all"}, nil, []string{"-j3", "all"}},
		{"combined", []string{"-ksj2", "all"}, nil, []string{"-j2", "-ks", "all"}},
		{"symlink combined", []string{"-Lj3", "all"}, nil, []string{"-j3", "-L", "all"}},
		{"compatibility combined", []string{"-mj2", "all"}, nil, []string{"-j2", "-m", "all"}},
		{"multiple combined", []string{"-Lmkj", "2", "all"}, nil, []string{"-j2", "-Lmk", "all"}},
		{"flags", []string{"all"}, map[string]string{"MAKEFLAGS": "ks -j2"}, []string{"-j2", "all"}},
		{"mflags", []string{"all"}, map[string]string{"MFLAGS": "-j3"}, []string{"-j3", "all"}},
		{"gnu", []string{"all"}, map[string]string{"GNUMAKEFLAGS": "-j1"}, []string{"-j1", "all"}},
		{"combined flags", []string{"all"}, map[string]string{"MAKEFLAGS": "Lmj2"}, []string{"-j2", "all"}},
		{"escaped flags", []string{"all"}, map[string]string{"MAKEFLAGS": "-Iwith\\ -j99"}, []string{"-j4", "all"}},
		{"escaped flags with jobs", []string{"all"}, map[string]string{"MAKEFLAGS": "-Iwith\\ -j99 -Lj2"}, []string{"-j2", "all"}},
		{"smaller environment", []string{"-j3", "all"}, map[string]string{"MAKEFLAGS": "-j2"}, []string{"-j2", "all"}},
		{"directory value", []string{"-C", "-j20", "all"}, nil, []string{"-j4", "-C", "-j20", "all"}},
		{"combined file value", []string{"-kf", "-j99", "all"}, nil, []string{"-j4", "-kf", "-j99", "all"}},
		{"attached file value", []string{"-kf-j99", "all"}, nil, []string{"-j4", "-kf-j99", "all"}},
		{"combined eval value", []string{"-kE", "VALUE=-j99", "all"}, nil, []string{"-j4", "-kE", "VALUE=-j99", "all"}},
		{"attached eval value", []string{"-kEVALUE=-j99", "all"}, nil, []string{"-j4", "-kEVALUE=-j99", "all"}},
		{"combined include value", []string{"-kI", "-j99", "all"}, nil, []string{"-j4", "-kI", "-j99", "all"}},
		{"combined old file value", []string{"-ko", "-j99", "all"}, nil, []string{"-j4", "-ko", "-j99", "all"}},
		{"combined new file value", []string{"-kW", "-j99", "all"}, nil, []string{"-j4", "-kW", "-j99", "all"}},
		{"attached output sync value", []string{"-kOj99", "all"}, nil, []string{"-j4", "-kOj99", "all"}},
		{"separate output sync", []string{"-kO", "-Lj2", "all"}, nil, []string{"-j2", "-kO", "-L", "all"}},
		{"attached load value", []string{"-klj99", "all"}, nil, []string{"-j4", "-klj99", "all"}},
		{"separate load value", []string{"-kl", "2.5", "-Lj2", "all"}, nil, []string{"-j2", "-kl", "2.5", "-L", "all"}},
		{"bare load before removed jobs", []string{"-l", "-j2", "5"}, nil, []string{"-j2", "-l-1", "5"}},
		{"long load before removed jobs", []string{"--load-average", "-j2", "5"}, nil, []string{"-j2", "--load-average=-1", "5"}},
		{"max load before removed jobs", []string{"--max-load", "-j2", "5"}, nil, []string{"-j2", "--max-load=-1", "5"}},
		{"end options", []string{"--", "-j20"}, nil, []string{"-j4", "--", "-j20"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := append([]string{}, test.args...)
			got, err := budget.MakeArgs(test.args, test.env)
			if err != nil || !slices.Equal(got, test.want) {
				t.Fatalf("arguments = %q, %v; want %q", got, err, test.want)
			}
			if !slices.Equal(test.args, before) {
				t.Fatal("mutated caller arguments")
			}
		})
	}
}

func TestRejectInvalidMakeParallelism(t *testing.T) {
	budget := Budget{Jobs: 4}
	for _, args := range [][]string{
		{"-j"}, {"-j", "all"}, {"-j0"}, {"-j-2"}, {"-j5"}, {"-j=2"},
		{"--jobs"}, {"--jobs="}, {"--jobs=5"}, {"-ksj"}, {"-ksj5"},
		{"-Lj5"}, {"-mj5"}, {"-Lmj5"}, {"-Lmj"}, {"-Lmj0"}, {"-Lmj-1"},
		{"-Lj2", "-mj3"},
		{"-j2", "--jobs=3"}, {"MAKEFLAGS=-j9"}, {"GNUMAKEFLAGS=-j9"},
		{"MAKEFLAGS:=-j9"}, {"MFLAGS += -j9"},
		{"--", "MAKEFLAGS=-j9"}, {"--", "GNUMAKEFLAGS:=-j9"},
		{"--jobserver-auth=3,4"}, {"--jobserver-fds=3,4"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if got, err := budget.MakeArgs(args, nil); err == nil {
				t.Fatalf("accepted %q as %q", args, got)
			}
		})
	}
	for _, key := range []string{"MAKEFLAGS", "MFLAGS", "GNUMAKEFLAGS"} {
		for _, flags := range []string{"-j", "-j5", "ksj5", "Lmj5", "-Lj", "-mj0", "-Lj2 -mj3", "--jobserver-auth=3,4", "-j2 -j3"} {
			if _, err := budget.MakeArgs(nil, map[string]string{key: flags}); err == nil {
				t.Errorf("accepted %s=%q", key, flags)
			}
		}
	}
}

func TestCMakeInstallBudget(t *testing.T) {
	budget := Budget{Jobs: 4}
	if got, err := budget.CMakeInstallArgs([]string{"--component", "Runtime"}, nil); err != nil || !slices.Equal(got, []string{"--component", "Runtime"}) {
		t.Fatalf("serial install changed: %q, %v", got, err)
	}
	if got, err := budget.CMakeInstallArgs([]string{"-j3"}, map[string]string{"CMAKE_INSTALL_PARALLEL_LEVEL": "2"}); err != nil || !slices.Equal(got, []string{"--parallel", "2"}) {
		t.Fatalf("install parallelism: %q, %v", got, err)
	}
	for _, value := range []string{"", "0", "-1", "5"} {
		if _, err := budget.CMakeInstallArgs(nil, map[string]string{"CMAKE_INSTALL_PARALLEL_LEVEL": value}); err == nil {
			t.Errorf("accepted CMAKE_INSTALL_PARALLEL_LEVEL=%q", value)
		}
	}
}

func TestCMakeBudget(t *testing.T) {
	budget := Budget{Jobs: 4}
	for _, test := range []struct {
		name string
		args []string
		env  map[string]string
		want []string
	}{
		{"default", nil, nil, []string{"--parallel", "4"}},
		{"explicit", []string{"--target", "app", "-j2"}, nil, []string{"--parallel", "2", "--target", "app"}},
		{"environment", nil, map[string]string{"CMAKE_BUILD_PARALLEL_LEVEL": "3"}, []string{"--parallel", "3"}},
		{"backend", []string{"--", "-j2", "--verbose"}, nil, []string{"--parallel", "2", "--", "--verbose"}},
		{"combined backend", []string{"--", "-Lmj2", "all"}, nil, []string{"--parallel", "2", "--", "-Lm", "all"}},
		{"backend file value", []string{"--", "-kf", "-j99"}, nil, []string{"--parallel", "4", "--", "-kf", "-j99"}},
		{"backend load before removed jobs", []string{"--", "-l", "-j2", "5"}, nil, []string{"--parallel", "2", "--", "-l-1", "5"}},
		{"combined make environment", nil, map[string]string{"MAKEFLAGS": "Lmj2"}, []string{"--parallel", "2"}},
		{"escaped make environment", nil, map[string]string{"MAKEFLAGS": "-Iwith\\ -j99"}, []string{"--parallel", "4"}},
		{"all limits", []string{"-j3", "--", "--jobs=2"}, map[string]string{"MAKEFLAGS": "-j1"}, []string{"--parallel", "1", "--"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := budget.CMakeBuildArgs(test.args, test.env)
			if err != nil || !slices.Equal(got, test.want) {
				t.Fatalf("arguments = %q, %v; want %q", got, err, test.want)
			}
		})
	}
	for _, args := range [][]string{
		{"--parallel"}, {"--parallel="}, {"--parallel", "0"}, {"-j5"},
		{"--", "-j"}, {"--", "-j0"}, {"--", "-j5"}, {"--", "MAKEFLAGS=-j8"},
		{"--", "-Lj5"}, {"--", "-mj5"}, {"--", "-Lmj"}, {"--", "-Lj2", "-mj3"},
		{"--parallel=2", "-j3"},
		{"--target", "-j9"}, {"--target", "MAKEFLAGS=-j9"},
		{"--", "--", "MAKEFLAGS=-j9"},
	} {
		if got, err := budget.CMakeBuildArgs(args, nil); err == nil {
			t.Errorf("accepted %q as %q", args, got)
		}
	}
	for _, value := range []string{"", "0", "-1", "5", "abc"} {
		if _, err := budget.CMakeBuildArgs(nil, map[string]string{"CMAKE_BUILD_PARALLEL_LEVEL": value}); err == nil {
			t.Errorf("accepted CMAKE_BUILD_PARALLEL_LEVEL=%q", value)
		}
	}
}

func TestMakeBudgetWithGNUMake(t *testing.T) {
	nativeJobs := regexp.MustCompile(`(?:^|[^\\])\s(-j[0-9]*)(?:\s|$)`)
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Skip("make is unavailable")
	}
	version, err := exec.Command(makePath, "--version").CombinedOutput()
	if err != nil || !strings.Contains(string(version), "GNU Make") {
		t.Skip("GNU Make is unavailable")
	}
	help, err := exec.Command(makePath, "--help").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"MAKEFLAGS", "MFLAGS", "GNUMAKEFLAGS", "MAKELEVEL", "MAKEOVERRIDES", "MAKEFILES"} {
		t.Setenv(key, "")
	}
	budget := Budget{Jobs: 4}
	for _, test := range []struct {
		name string
		args []string
		env  map[string]string
		jobs int
		goal string
	}{
		{"symlink combined", []string{"-Lj3", "all"}, nil, 3, "all"},
		{"compatibility combined", []string{"-mj2", "all"}, nil, 2, "all"},
		{"combined separate count", []string{"-Lmkj", "2", "all"}, nil, 2, "all"},
		{"file value", []string{"-kf", "-j99", "all"}, nil, 4, "all"},
		{"attached file value", []string{"-kf-j99", "all"}, nil, 4, "all"},
		{"eval value", []string{"-kE", "VALUE=-j99", "all"}, nil, 4, "all"},
		{"attached eval value", []string{"-kEVALUE=-j99", "all"}, nil, 4, "all"},
		{"output sync", []string{"-kO", "-Lj2", "all"}, nil, 2, "all"},
		{"load goal", []string{"-l", "-j2", "5"}, nil, 2, "5"},
		{"combined load goal", []string{"-kl", "-j2", "5"}, nil, 2, "5"},
		{"long load goal", []string{"--load-average", "-j2", "5"}, nil, 2, "5"},
		{"max load goal", []string{"--max-load", "-j2", "5"}, nil, 2, "5"},
		{"lower environment", []string{"-Lj3", "all"}, map[string]string{"MAKEFLAGS": "Lmj2"}, 2, "all"},
		{"escaped environment", []string{"all"}, map[string]string{"MAKEFLAGS": "-Iwith\\ -j99"}, 4, "all"},
		{"escaped environment and jobs", []string{"all"}, map[string]string{"MAKEFLAGS": "-Iwith\\ -j99 -Lmj2"}, 2, "all"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, arg := range test.args {
				if strings.HasPrefix(arg, "-kE") && !strings.Contains(string(help), "-E ") {
					t.Skip("GNU Make does not support eval")
				}
				if arg == "-kO" && !strings.Contains(string(help), "--output-sync") {
					t.Skip("GNU Make does not support output sync")
				}
			}
			dir := t.TempDir()
			makefile := ".PHONY: all 5\nall 5:\n\t$(info VMAKE_TEST_FLAGS=$(MAKEFLAGS))\n\t$(info VMAKE_TEST_GOAL=$@)\n"
			for _, file := range []string{"Makefile", "-j99"} {
				if err := os.WriteFile(filepath.Join(dir, file), []byte(makefile), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			args, err := budget.MakeArgs(test.args, test.env)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(makePath, args...)
			cmd.Dir = dir
			cmd.Env = os.Environ()
			for key, value := range test.env {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("make %q: %v\n%s", args, err, output)
			}
			var flags, goal string
			for _, line := range strings.Split(string(output), "\n") {
				if value, ok := strings.CutPrefix(line, "VMAKE_TEST_FLAGS="); ok {
					flags = value
				}
				if value, ok := strings.CutPrefix(line, "VMAKE_TEST_GOAL="); ok {
					goal = strings.TrimSpace(value)
				}
			}
			matches := nativeJobs.FindAllStringSubmatch(flags, -1)
			if want := "-j" + strconv.Itoa(test.jobs); len(matches) != 1 || matches[0][1] != want {
				t.Fatalf("native MAKEFLAGS = %q, effective jobs = %q; want %q", flags, matches, want)
			}
			if goal != test.goal {
				t.Fatalf("native goal = %q; want %q", goal, test.goal)
			}
		})
	}
}
