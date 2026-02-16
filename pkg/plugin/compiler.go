package plugin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
		return compileWithGoMod(pkg, pluginPath, vmakeDir)
	}
	return compileWithGopath(pkg, pluginPath)
}

func compileWithGoMod(pkg Package, pluginPath, vmakeDir string) CompileResult {
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

func compileWithGopath(pkg Package, pluginPath string) CompileResult {
	vmakePath, err := getVMakePath()
	if err != nil {
		return CompileResult{
			Package: pkg,
			Success: false,
			Error:   err,
		}
	}

	userGopath, err := getUserGopath()
	if err != nil {
		return CompileResult{
			Package: pkg,
			Success: false,
			Error:   err,
		}
	}

	srcDir := filepath.Join(userGopath, "src", "gitee.com", "spock2300")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		return CompileResult{
			Package: pkg,
			Success: false,
			Error:   fmt.Errorf("failed to create src directory: %w", err),
		}
	}

	linkPath := filepath.Join(srcDir, "vmake")
	linkTarget, _ := os.Readlink(linkPath)
	if linkTarget != vmakePath {
		os.Remove(linkPath)
		if err := os.Symlink(vmakePath, linkPath); err != nil {
			return CompileResult{
				Package: pkg,
				Success: false,
				Error:   fmt.Errorf("failed to create symlink: %w", err),
			}
		}
	}

	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", pluginPath, "build.go")
	cmd.Dir = pkg.Dir
	cmd.Env = append(os.Environ(),
		"GO111MODULE=off",
		"GOPATH="+userGopath,
	)

	output, err := cmd.CombinedOutput()
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

func getVMakePath() (string, error) {
	if dir := os.Getenv("VMAKE_DIR"); dir != "" {
		return dir, nil
	}
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "gitee.com/spock2300/vmake")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("vmake module not found (run 'go install gitee.com/spock2300/vmake/cmd/vmake@latest' or set VMAKE_DIR): %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func getUserGopath() (string, error) {
	cmd := exec.Command("go", "env", "GOPATH")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get GOPATH: %w", err)
	}
	gopath := strings.TrimSpace(string(output))
	if gopath == "" {
		return "", fmt.Errorf("GOPATH is not set")
	}
	if idx := strings.Index(gopath, string(os.PathListSeparator)); idx > 0 {
		gopath = gopath[:idx]
	}
	return gopath, nil
}

func CompileAll(packages []Package) []CompileResult {
	results := make([]CompileResult, len(packages))
	for i, pkg := range packages {
		results[i] = Compile(pkg)
	}
	return results
}
