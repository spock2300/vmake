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
	PluginDir  string
	PluginName string
	OutputPath string
	Success    bool
	Error      error
}

func Compile(pluginDir string, force bool) CompileResult {
	info, err := LoadPluginInfo(pluginDir)
	if err != nil {
		return CompileResult{
			PluginDir: pluginDir,
			Success:   false,
			Error:     err,
		}
	}

	entryPath := filepath.Join(pluginDir, info.Entry)
	if _, err := os.Stat(entryPath); err != nil {
		return CompileResult{
			PluginDir:  pluginDir,
			PluginName: info.Name,
			Success:    false,
			Error:      fmt.Errorf("entry file not found: %s", entryPath),
		}
	}

	outputPath := filepath.Join(pluginDir, "plugin.so")

	if force {
		os.Remove(outputPath)
	}

	if _, err := os.Stat(outputPath); err == nil {
		return CompileResult{
			PluginDir:  pluginDir,
			PluginName: info.Name,
			OutputPath: outputPath,
			Success:    true,
		}
	}

	vmakeDir := os.Getenv("VMAKE_DIR")
	if vmakeDir != "" {
		return compileWithGoModReplace(pluginDir, entryPath, outputPath, info.Name, vmakeDir)
	}
	return compileWithGoModVersion(pluginDir, entryPath, outputPath, info.Name)
}

func compileWithGoModReplace(pluginDir, entryPath, outputPath, pluginName, vmakeDir string) CompileResult {
	moduleName := "vmake_plugin_" + sanitizeModuleName(pluginName)
	workDir := filepath.Dir(entryPath)

	goModContent := fmt.Sprintf(`module %s

go 1.22

require gitee.com/spock2300/vmake v0.0.0

replace gitee.com/spock2300/vmake => %s
`, moduleName, vmakeDir)

	return buildPlugin(pluginDir, workDir, outputPath, pluginName, goModContent)
}

func compileWithGoModVersion(pluginDir, entryPath, outputPath, pluginName string) CompileResult {
	vmakeVersion := version.Version
	if vmakeVersion == "dev" {
		vmakeVersion = "latest"
	}

	moduleName := "vmake_plugin_" + sanitizeModuleName(pluginName)
	workDir := filepath.Dir(entryPath)

	goModContent := fmt.Sprintf(`module %s

go 1.22

require gitee.com/spock2300/vmake %s
`, moduleName, vmakeVersion)

	return buildPlugin(pluginDir, workDir, outputPath, pluginName, goModContent)
}

func buildPlugin(pluginDir, workDir, outputPath, pluginName, goModContent string) CompileResult {
	goModPath := filepath.Join(workDir, "go.mod")
	needCleanup := false

	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil {
			return CompileResult{
				PluginDir:  pluginDir,
				PluginName: pluginName,
				Success:    false,
				Error:      fmt.Errorf("failed to write go.mod: %w", err),
			}
		}
		needCleanup = true
	}

	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = workDir
	tidyCmd.Env = append(os.Environ(), "GO111MODULE=on")
	if tidyOutput, err := tidyCmd.CombinedOutput(); err != nil {
		if needCleanup {
			os.Remove(goModPath)
		}
		return CompileResult{
			PluginDir:  pluginDir,
			PluginName: pluginName,
			Success:    false,
			Error:      fmt.Errorf("go mod tidy failed: %s", string(tidyOutput)),
		}
	}

	entryFile := filepath.Base(filepath.Join(pluginDir, "src", "main.go"))
	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", outputPath, entryFile)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "GO111MODULE=on")

	output, err := cmd.CombinedOutput()

	if needCleanup {
		os.Remove(goModPath)
		os.Remove(filepath.Join(workDir, "go.sum"))
	}

	if err != nil {
		return CompileResult{
			PluginDir:  pluginDir,
			PluginName: pluginName,
			OutputPath: outputPath,
			Success:    false,
			Error:      fmt.Errorf("compilation failed: %s", string(output)),
		}
	}

	return CompileResult{
		PluginDir:  pluginDir,
		PluginName: pluginName,
		OutputPath: outputPath,
		Success:    true,
	}
}

func sanitizeModuleName(name string) string {
	return strings.ReplaceAll(name, "/", "_")
}
