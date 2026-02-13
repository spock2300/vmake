package plugin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type CompileResult struct {
	Package    Package
	PluginPath string
	Success    bool
	Error      error
}

func Compile(pkg Package) CompileResult {
	buildDir := filepath.Join(pkg.Dir, "build")
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return CompileResult{
			Package: pkg,
			Success: false,
			Error:   fmt.Errorf("failed to create build directory: %w", err),
		}
	}

	pluginPath := filepath.Join(buildDir, "plugin.so")

	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", pluginPath, pkg.Path)
	cmd.Dir = pkg.Dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return CompileResult{
			Package:    pkg,
			PluginPath: pluginPath,
			Success:    false,
			Error:      fmt.Errorf("compilation failed: %w\n%s", err, string(output)),
		}
	}

	return CompileResult{
		Package:    pkg,
		PluginPath: pluginPath,
		Success:    true,
	}
}

func CompileAll(packages []Package) []CompileResult {
	results := make([]CompileResult, len(packages))
	for i, pkg := range packages {
		results[i] = Compile(pkg)
	}
	return results
}
