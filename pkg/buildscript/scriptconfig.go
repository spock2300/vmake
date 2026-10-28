package buildscript

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ScriptOptions describes the local Go import root available to one build
// script. It is bound to a Source, so a remote package can only resolve its
// own checkout and can never see the consuming project's helpers.
type ScriptOptions struct {
	ProjectDir string
	ModulePath string
	ModuleRoot string
}

type vmakeScriptConfig struct {
	Script *scriptSection `json:"script"`
}

type scriptSection struct {
	Module string `json:"module"`
	Root   string `json:"root"`
}

var goModuleLine = regexp.MustCompile(`(?m)^module[ \t]+([^ \t\r\n]+)`)

// DetectScriptOptions finds the nearest vmake.json, .vmake/script.json or
// go.mod between startDir and stopDir. stopDir is the local project root or a
// remote checkout root; the search never crosses it.
func DetectScriptOptions(startDir, stopDir string) (ScriptOptions, error) {
	if startDir == "" {
		return ScriptOptions{}, nil
	}
	start, err := filepath.Abs(startDir)
	if err != nil {
		return ScriptOptions{}, fmt.Errorf("resolve script start dir %s: %w", startDir, err)
	}
	if stopDir == "" {
		stopDir = start
	}
	stop, err := filepath.Abs(stopDir)
	if err != nil {
		return ScriptOptions{}, fmt.Errorf("resolve script root %s: %w", stopDir, err)
	}
	if !withinDir(stop, start) {
		return ScriptOptions{}, fmt.Errorf("script dir %s is outside module root %s", start, stop)
	}

	for dir := start; ; dir = filepath.Dir(dir) {
		opts, found, err := readScriptConfig(dir, stop)
		if err != nil {
			return ScriptOptions{}, err
		}
		if found {
			opts.ProjectDir = stop
			return opts, nil
		}
		if filepath.Clean(dir) == filepath.Clean(stop) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir || !withinDir(stop, parent) {
			break
		}
	}
	return ScriptOptions{ProjectDir: stop}, nil
}

func readScriptConfig(dir, stop string) (ScriptOptions, bool, error) {
	if opts, found, err := readVmakeJSON(filepath.Join(dir, "vmake.json"), dir, stop); err != nil || found {
		return opts, found, err
	}
	if opts, found, err := readVmakeJSON(filepath.Join(dir, ".vmake", "script.json"), dir, stop); err != nil || found {
		return opts, found, err
	}
	path := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ScriptOptions{}, false, nil
	}
	if err != nil {
		return ScriptOptions{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	match := goModuleLine.FindSubmatch(data)
	if len(match) != 2 {
		return ScriptOptions{}, false, fmt.Errorf("%s has no module declaration", path)
	}
	return makeScriptOptions(string(match[1]), ".", dir, stop)
}

func readVmakeJSON(path, configDir, stop string) (ScriptOptions, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ScriptOptions{}, false, nil
	}
	if err != nil {
		return ScriptOptions{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg vmakeScriptConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ScriptOptions{}, false, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Script == nil {
		return ScriptOptions{}, false, nil
	}
	return makeScriptOptions(cfg.Script.Module, cfg.Script.Root, configDir, stop)
}

func makeScriptOptions(module, root, configDir, stop string) (ScriptOptions, bool, error) {
	module = strings.TrimSpace(module)
	if module == "" {
		return ScriptOptions{}, false, fmt.Errorf("%s: script.module must not be empty", filepath.Join(configDir, "vmake.json"))
	}
	if !validModulePath(module) {
		return ScriptOptions{}, false, fmt.Errorf("invalid script.module %q", module)
	}
	if root == "" {
		root = "."
	}
	if filepath.IsAbs(root) {
		return ScriptOptions{}, false, fmt.Errorf("script.root must be relative, got %q", root)
	}
	moduleRoot := filepath.Join(configDir, filepath.FromSlash(root))
	moduleRoot, err := filepath.Abs(moduleRoot)
	if err != nil {
		return ScriptOptions{}, false, err
	}
	if !withinDir(stop, moduleRoot) {
		return ScriptOptions{}, false, fmt.Errorf("script.root %q leaves %s", root, stop)
	}
	info, err := os.Stat(moduleRoot)
	if err != nil {
		return ScriptOptions{}, false, fmt.Errorf("script.root %q: %w", root, err)
	}
	if !info.IsDir() {
		return ScriptOptions{}, false, fmt.Errorf("script.root %q is not a directory", root)
	}
	realStop, err := filepath.EvalSymlinks(stop)
	if err != nil {
		return ScriptOptions{}, false, fmt.Errorf("resolve module boundary %s: %w", stop, err)
	}
	realRoot, err := filepath.EvalSymlinks(moduleRoot)
	if err != nil {
		return ScriptOptions{}, false, fmt.Errorf("resolve script.root %q: %w", root, err)
	}
	if !withinDir(realStop, realRoot) {
		return ScriptOptions{}, false, fmt.Errorf("script.root %q leaves %s after resolving symlinks", root, stop)
	}
	return ScriptOptions{ModulePath: module, ModuleRoot: moduleRoot}, true, nil
}

func validModulePath(module string) bool {
	if strings.ContainsAny(module, "\\ \t\r\n") || strings.HasPrefix(module, "/") ||
		strings.HasSuffix(module, "/") {
		return false
	}
	for _, part := range strings.Split(module, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func withinDir(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if root == path {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}
