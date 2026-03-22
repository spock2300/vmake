package plugin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gitee.com/spock2300/vmake/pkg/version"
)

type CompileResult struct {
	Source     Source
	PluginPath string
	Success    bool
	Error      error
}

func Compile(src Source) CompileResult {
	outputDir := src.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(src.Dir, "build")
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return CompileResult{
			Source:  src,
			Success: false,
			Error:   fmt.Errorf("failed to create build directory: %w", err),
		}
	}

	pluginPath := filepath.Join(outputDir, "plugin.so")

	if src.Force {
		os.Remove(pluginPath)
	}

	vmakeDir := os.Getenv("VMAKE_DIR")
	if vmakeDir != "" {
		return compileWithGoModReplace(src, pluginPath, src.Dir, vmakeDir)
	}
	return compileWithGoModVersion(src, pluginPath, src.Dir)
}

func writeGoModIfMissing(workDir, content string) bool {
	goModPath := filepath.Join(workDir, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		os.WriteFile(goModPath, []byte(content), 0644)
		return true
	}
	return false
}

func buildPlugin(src Source, pluginPath, workDir string, needCleanup bool) CompileResult {
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = workDir
	tidyCmd.Env = append(os.Environ(), "GO111MODULE=on")
	if tidyOutput, err := tidyCmd.CombinedOutput(); err != nil {
		if needCleanup {
			os.Remove(filepath.Join(workDir, "go.mod"))
		}
		return CompileResult{
			Source:  src,
			Success: false,
			Error:   fmt.Errorf("go mod tidy failed: %s", string(tidyOutput)),
		}
	}

	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", pluginPath, "build.go")
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "GO111MODULE=on")

	output, err := cmd.CombinedOutput()

	if needCleanup {
		os.Remove(filepath.Join(workDir, "go.mod"))
		os.Remove(filepath.Join(workDir, "go.sum"))
	}

	if err != nil {
		return CompileResult{
			Source:     src,
			PluginPath: pluginPath,
			Success:    false,
			Error:      fmt.Errorf("compilation failed: %s", string(output)),
		}
	}

	return CompileResult{
		Source:     src,
		PluginPath: pluginPath,
		Success:    true,
	}
}

func compileWithGoModReplace(src Source, pluginPath, workDir, vmakeDir string) CompileResult {
	moduleName := "vmake_plugin_" + sanitizeModuleName(src.Name)
	goModContent := fmt.Sprintf(`module %s

go 1.22

require gitee.com/spock2300/vmake v0.0.0

replace gitee.com/spock2300/vmake => %s
`, moduleName, vmakeDir)

	needCleanup := writeGoModIfMissing(workDir, goModContent)
	return buildPlugin(src, pluginPath, workDir, needCleanup)
}

func compileWithGoModVersion(src Source, pluginPath, workDir string) CompileResult {
	vmakeVersion := version.Version
	if vmakeVersion == "dev" {
		vmakeVersion = "latest"
	}

	moduleName := "vmake_plugin_" + sanitizeModuleName(src.Name)
	goModContent := fmt.Sprintf(`module %s

go 1.22

require gitee.com/spock2300/vmake %s
`, moduleName, vmakeVersion)

	needCleanup := writeGoModIfMissing(workDir, goModContent)
	return buildPlugin(src, pluginPath, workDir, needCleanup)
}

func CompileAll(sources []Source) []CompileResult {
	results := make([]CompileResult, len(sources))
	for i, src := range sources {
		results[i] = Compile(src)
	}
	return results
}

func sanitizeModuleName(name string) string {
	return strings.ReplaceAll(name, "/", "_")
}
