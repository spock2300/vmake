package plugin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gitee.com/spock2300/vmake/pkg/version"
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

	vmakeDir := os.Getenv("VMAKE_DIR")

	if vmakeDir != "" {
		return compileWithGoModReplace(pkg, pluginPath, vmakeDir)
	}
	return compileWithGoModVersion(pkg, pluginPath)
}

func compileWithGoModReplace(pkg Package, pluginPath, vmakeDir string) CompileResult {
	goModPath := filepath.Join(pkg.Dir, "go.mod")
	goModContent := fmt.Sprintf(`module vmake_plugin

go 1.22

require gitee.com/spock2300/vmake v0.0.0

replace gitee.com/spock2300/vmake => %s
`, vmakeDir)

	needCleanup := false
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil {
			return CompileResult{
				Package: pkg,
				Success: false,
				Error:   fmt.Errorf("failed to create go.mod: %w", err),
			}
		}
		needCleanup = true
	}

	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = pkg.Dir
	tidyCmd.Env = append(os.Environ(), "GO111MODULE=on")
	if tidyOutput, err := tidyCmd.CombinedOutput(); err != nil {
		if needCleanup {
			os.Remove(goModPath)
		}
		return CompileResult{
			Package: pkg,
			Success: false,
			Error:   fmt.Errorf("go mod tidy failed: %s", string(tidyOutput)),
		}
	}

	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", pluginPath, "build.go")
	cmd.Dir = pkg.Dir
	cmd.Env = append(os.Environ(), "GO111MODULE=on")

	output, err := cmd.CombinedOutput()

	if needCleanup {
		os.Remove(goModPath)
		os.Remove(filepath.Join(pkg.Dir, "go.sum"))
	}

	if err != nil {
		return CompileResult{
			Package:    pkg,
			PluginPath: pluginPath,
			Success:    false,
			Error:      fmt.Errorf("compilation failed: %s", string(output)),
		}
	}

	return CompileResult{
		Package:    pkg,
		PluginPath: pluginPath,
		Success:    true,
	}
}

func compileWithGoModVersion(pkg Package, pluginPath string) CompileResult {
	vmakeVersion := version.Version
	if vmakeVersion == "dev" {
		vmakeVersion = "latest"
	}

	goModPath := filepath.Join(pkg.Dir, "go.mod")
	goModContent := fmt.Sprintf(`module vmake_plugin

go 1.22

require gitee.com/spock2300/vmake %s
`, vmakeVersion)

	needCleanup := false
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil {
			return CompileResult{
				Package: pkg,
				Success: false,
				Error:   fmt.Errorf("failed to create go.mod: %w", err),
			}
		}
		needCleanup = true
	}

	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = pkg.Dir
	tidyCmd.Env = append(os.Environ(), "GO111MODULE=on")
	if tidyOutput, err := tidyCmd.CombinedOutput(); err != nil {
		if needCleanup {
			os.Remove(goModPath)
		}
		return CompileResult{
			Package: pkg,
			Success: false,
			Error:   fmt.Errorf("go mod tidy failed: %s", string(tidyOutput)),
		}
	}

	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", pluginPath, "build.go")
	cmd.Dir = pkg.Dir
	cmd.Env = append(os.Environ(), "GO111MODULE=on")

	output, err := cmd.CombinedOutput()

	if needCleanup {
		os.Remove(goModPath)
		os.Remove(filepath.Join(pkg.Dir, "go.sum"))
	}

	if err != nil {
		return CompileResult{
			Package:    pkg,
			PluginPath: pluginPath,
			Success:    false,
			Error:      fmt.Errorf("compilation failed: %s", string(output)),
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
