package buildruntime

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
)

type Budget struct {
	Jobs int
}

func NewBudget(jobs int) *Budget {
	if jobs <= 0 {
		jobs = runtime.NumCPU()
	}
	if jobs < 1 {
		jobs = 1
	}
	return &Budget{Jobs: jobs}
}

func (b Budget) jobs() int {
	if b.Jobs <= 0 {
		if n := runtime.NumCPU(); n > 0 {
			return n
		}
		return 1
	}
	return b.Jobs
}

func (b Budget) MakeArgs(args []string, env map[string]string) ([]string, error) {
	if b.Jobs < 1 {
		return nil, fmt.Errorf("build jobs budget must be positive, got %d", b.Jobs)
	}
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "-j" || arg == "--jobs" || (strings.HasPrefix(arg, "-j") && !strings.HasPrefix(arg, "--")) || strings.HasPrefix(arg, "--jobs=") {
			return append([]string(nil), args...), nil
		}
	}
	for _, key := range []string{"MAKEFLAGS", "GNUMAKEFLAGS", "MFLAGS"} {
		if v := env[key]; strings.Contains(v, "-j") || strings.Contains(v, "--jobs") {
			return append([]string(nil), args...), nil
		}
	}
	return append([]string{fmt.Sprintf("-j%d", b.jobs())}, args...), nil
}

func (b Budget) CMakeBuildArgs(args []string, env map[string]string) ([]string, error) {
	if b.Jobs < 1 {
		return nil, fmt.Errorf("build jobs budget must be positive, got %d", b.Jobs)
	}
	split := len(args)
	for i, arg := range args {
		if arg == "--" {
			split = i
			break
		}
		if arg == "--parallel" || arg == "-j" || (strings.HasPrefix(arg, "-j") && !strings.HasPrefix(arg, "--")) || strings.HasPrefix(arg, "--parallel=") {
			return append([]string(nil), args...), nil
		}
	}
	if strings.TrimSpace(env["CMAKE_BUILD_PARALLEL_LEVEL"]) != "" {
		return append([]string(nil), args...), nil
	}
	result := make([]string, 0, len(args)+2)
	result = append(result, args[:split]...)
	result = append(result, "--parallel", strconv.Itoa(b.jobs()))
	result = append(result, args[split:]...)
	return result, nil
}

func (b Budget) CMakeInstallArgs(args []string, env map[string]string) ([]string, error) {
	if b.Jobs < 1 {
		return nil, fmt.Errorf("build jobs budget must be positive, got %d", b.Jobs)
	}
	return append([]string(nil), args...), nil
}
