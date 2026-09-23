package buildruntime

import (
	"fmt"
	"strconv"
	"strings"
)

type Budget struct {
	Jobs int
}

func (b Budget) MakeArgs(args []string, env map[string]string) ([]string, error) {
	jobs, err := b.environmentJobs(env, false)
	if err != nil {
		return nil, err
	}
	rest, requested, err := b.jobArgs(args, true)
	if err != nil {
		return nil, err
	}
	jobs = min(jobs, requested)
	return append([]string{"-j" + strconv.Itoa(jobs)}, rest...), nil
}

func (b Budget) CMakeBuildArgs(args []string, env map[string]string) ([]string, error) {
	jobs, err := b.environmentJobs(env, true)
	if err != nil {
		return nil, err
	}
	front, backend, separated := splitBackend(args)
	front, requested, err := b.jobArgs(front, false)
	if err != nil {
		return nil, err
	}
	jobs = min(jobs, requested)
	backend, requested, err = b.jobArgs(backend, true)
	if err != nil {
		return nil, fmt.Errorf("CMake native build arguments: %w", err)
	}
	jobs = min(jobs, requested)
	result := append([]string{"--parallel", strconv.Itoa(jobs)}, front...)
	if separated {
		result = append(result, "--")
		result = append(result, backend...)
	}
	return result, nil
}

func (b Budget) CMakeInstallArgs(args []string, env map[string]string) ([]string, error) {
	if b.Jobs < 1 {
		return nil, fmt.Errorf("build jobs budget must be positive, got %d", b.Jobs)
	}
	rest, jobs, err := b.jobArgs(args, false)
	if err != nil {
		return nil, err
	}
	requested := len(rest) != len(args)
	if value, exists := env["CMAKE_INSTALL_PARALLEL_LEVEL"]; exists {
		count, err := b.count(value, "CMAKE_INSTALL_PARALLEL_LEVEL")
		if err != nil {
			return nil, err
		}
		jobs = min(jobs, count)
		requested = true
	}
	if requested {
		return append([]string{"--parallel", strconv.Itoa(jobs)}, rest...), nil
	}
	return rest, nil
}

func (b Budget) environmentJobs(env map[string]string, cmake bool) (int, error) {
	if b.Jobs < 1 {
		return 0, fmt.Errorf("build jobs budget must be positive, got %d", b.Jobs)
	}
	jobs := b.Jobs
	for _, key := range []string{"MAKEFLAGS", "MFLAGS", "GNUMAKEFLAGS"} {
		flags := makeEnvironmentArgs(env[key])
		if len(flags) > 0 && !strings.HasPrefix(flags[0], "-") && !strings.Contains(flags[0], "=") {
			flags[0] = "-" + flags[0]
		}
		_, requested, err := b.jobArgs(flags, true)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", key, err)
		}
		jobs = min(jobs, requested)
	}
	if cmake {
		if value, exists := env["CMAKE_BUILD_PARALLEL_LEVEL"]; exists {
			requested, err := b.count(value, "CMAKE_BUILD_PARALLEL_LEVEL")
			if err != nil {
				return 0, err
			}
			jobs = min(jobs, requested)
		}
	}
	return jobs, nil
}

func (b Budget) jobArgs(args []string, makeArgs bool) ([]string, int, error) {
	var result []string
	jobs := b.Jobs
	requested := 0
	options := true
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			options = false
			result = append(result, arg)
			continue
		}
		if key, _, assignment := strings.Cut(arg, "="); assignment {
			key = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(key), ":+?!"))
			switch key {
			case "MAKEFLAGS", "MFLAGS", "GNUMAKEFLAGS", "CMAKE_BUILD_PARALLEL_LEVEL", "CMAKE_INSTALL_PARALLEL_LEVEL":
				return nil, 0, fmt.Errorf("%s command assignments cannot override VMake's jobs budget", key)
			}
		}
		if !options {
			result = append(result, arg)
			continue
		}
		if strings.HasPrefix(arg, "--jobserver") {
			return nil, 0, fmt.Errorf("inherited jobserver arguments are unsupported; VMake manages the jobs budget")
		}
		value := ""
		jobOption := false
		needsValue := false
		takesValue := optionTakesValue(arg, makeArgs)
		switch {
		case arg == "-j" || arg == "--parallel" || makeArgs && arg == "--jobs":
			jobOption, needsValue = true, true
		case strings.HasPrefix(arg, "--parallel="):
			jobOption, value = true, strings.TrimPrefix(arg, "--parallel=")
		case makeArgs && strings.HasPrefix(arg, "--jobs="):
			jobOption, value = true, strings.TrimPrefix(arg, "--jobs=")
		case strings.HasPrefix(arg, "-j") && !strings.HasPrefix(arg, "--"):
			jobOption, value = true, strings.TrimPrefix(arg, "-j")
		case makeArgs && strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--"):
			option, at := makeShortValueOption(arg)
			switch option {
			case 'j':
				if at > 1 {
					result = append(result, arg[:at])
				}
				jobOption, value = true, arg[at+1:]
				needsValue = value == ""
			case 'C', 'E', 'f', 'I', 'o', 'W':
				takesValue = at+1 == len(arg)
			case 'l':
				if at+1 == len(arg) && !nextMakeLoadValue(args, i) {
					arg += "-1"
				}
			}
		case makeArgs && (arg == "--load-average" || arg == "--max-load"):
			if !nextMakeLoadValue(args, i) {
				arg += "=-1"
			}
		}
		if !jobOption {
			result = append(result, arg)
			if takesValue && i+1 < len(args) {
				i++
				result = append(result, args[i])
			}
			continue
		}
		if needsValue && i+1 < len(args) {
			i++
			value = args[i]
		}
		count, err := b.count(value, arg)
		if err != nil {
			return nil, 0, err
		}
		if requested != 0 && requested != count {
			return nil, 0, fmt.Errorf("conflicting parallelism values %d and %d", requested, count)
		}
		requested = count
		jobs = min(jobs, count)
	}
	return result, jobs, nil
}

func (b Budget) count(value, source string) (int, error) {
	count, err := strconv.Atoi(value)
	if err != nil || count < 1 || count > b.Jobs {
		return 0, fmt.Errorf("%s requires a positive job count at most %d, got %q", source, b.Jobs, value)
	}
	return count, nil
}

func makeShortValueOption(arg string) (byte, int) {
	for i := 1; i < len(arg); i++ {
		if strings.IndexByte("CEfIjloOW", arg[i]) >= 0 {
			return arg[i], i
		}
	}
	return 0, -1
}

func nextMakeLoadValue(args []string, i int) bool {
	if i+1 >= len(args) || args[i+1] == "" {
		return false
	}
	first := args[i+1][0]
	return first >= '0' && first <= '9' || first == '.'
}

func makeEnvironmentArgs(value string) []string {
	var args []string
	var word strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' && i+1 < len(value) {
			i++
		} else if value[i] == ' ' || value[i] == '\t' {
			if word.Len() > 0 {
				args = append(args, word.String())
				word.Reset()
			}
			continue
		}
		word.WriteByte(value[i])
	}
	if word.Len() > 0 {
		args = append(args, word.String())
	}
	return args
}

func optionTakesValue(option string, makeArgs bool) bool {
	if makeArgs {
		switch option {
		case "-C", "--directory", "-E", "--eval", "-f", "--file", "--makefile", "-I", "--include-dir", "-o", "--old-file", "--assume-old", "-W", "--what-if", "--new-file", "--assume-new", "--temp-stdin", "--sync-mutex":
			return true
		}
		return false
	}
	switch option {
	case "--config", "--preset", "--component":
		return true
	}
	return false
}

func splitBackend(args []string) ([]string, []string, bool) {
	for i, arg := range args {
		if arg == "--" {
			return args[:i], args[i+1:], true
		}
	}
	return args, nil, false
}
