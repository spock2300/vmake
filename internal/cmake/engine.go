package cmake

import (
	"fmt"
	"os"
	"path/filepath"
)

// Plan is one resolved cmake invocation: its arguments, the directory it runs
// in, the program, and (for configure) the fingerprint and whether the recorded
// stamp allows reuse.
type Plan struct {
	Args        []string
	Dir         string
	Executable  string
	fingerprint string
	Reuse       bool
	Preset      bool
}

// Engine owns cmake's incremental protocol for one Settings value: it plans an
// invocation, invalidates a stale stamp before running it, and records the
// stamp after success. Callers keep execution and output rendering.
type Engine struct {
	settings Settings
}

func NewEngine(s Settings) Engine {
	return Engine{settings: s}
}

func (e Engine) PlanConfigure(extra []string) (Plan, error) {
	args, err := ConfigureArgs(e.settings, extra...)
	if err != nil {
		return Plan{}, err
	}
	fingerprint, reuse := stampState(e.settings, args)
	return Plan{
		Args:        args,
		Dir:         configureDir(e.settings, extra),
		Executable:  e.executable(),
		fingerprint: fingerprint,
		Reuse:       reuse,
		Preset:      HasOption(extra, "--preset"),
	}, nil
}

// PlanBuild resolves the managed build invocation. extra is the caller's
// argument list after the job-ceiling merge; the managed build directory and
// build type always address the tree Configure planned.
func (e Engine) PlanBuild(extra []string) (Plan, error) {
	if err := validateArgs(extra); err != nil {
		return Plan{}, err
	}
	if HasOption(extra, "--preset") {
		return Plan{}, fmt.Errorf("CMake build presets override the managed build directory; pass configure presets to CMakeConfigure and use --target or --config with CMakeBuild")
	}
	args := []string{"--build", filepath.ToSlash(e.settings.BuildDir)}
	args = e.appendManagedConfig(args, extra)
	args = append(args, extra...)
	return Plan{Args: args, Dir: e.settings.BuildRoot, Executable: e.executable()}, nil
}

// PlanInstall resolves the managed install invocation.
func (e Engine) PlanInstall(extra []string) (Plan, error) {
	if err := validateArgs(extra); err != nil {
		return Plan{}, err
	}
	args := []string{
		"--install", filepath.ToSlash(e.settings.BuildDir),
		"--prefix", filepath.ToSlash(e.settings.InstallDir),
	}
	args = e.appendManagedConfig(args, extra)
	args = append(args, extra...)
	return Plan{Args: args, Dir: e.settings.BuildRoot, Executable: e.executable()}, nil
}

// appendManagedConfig adds the managed build configuration unless the caller
// selected one, so the configured mode always applies.
func (e Engine) appendManagedConfig(args, extra []string) []string {
	if HasOption(extra, "--config") || e.settings.BuildType == "" {
		return args
	}
	return append(args, "--config", e.settings.BuildType)
}

// executable is the one resolution of the cmake program, shared by every stage.
func (e Engine) executable() string {
	path, _ := ResolveExecutable(e.settings)
	if path == "" {
		return "cmake"
	}
	return path
}

// Invalidate drops a stamp left by an attempt that may have mutated
// CMakeCache.txt or CMakeFiles, so a later invocation cannot reuse a tree that
// no longer matches it.
func (e Engine) Invalidate() error {
	if err := os.Remove(stampPath(e.settings)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Commit records the fingerprint of a configure that just succeeded.
func (e Engine) Commit(p Plan) error {
	return writeStamp(e.settings, p.Args, p.fingerprint)
}
