package plugin

import (
	"fmt"
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/internal/gocompile"
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
	if !fs.FileExists(entryPath) {
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

	if fs.FileExists(outputPath) {
		return CompileResult{
			PluginDir:  pluginDir,
			PluginName: info.Name,
			OutputPath: outputPath,
			Success:    true,
		}
	}

	entryFile := filepath.Base(filepath.Join(pluginDir, "src", "main.go"))
	workDir := filepath.Dir(entryPath)

	opts := gocompile.PluginOptions{
		WorkDir:    workDir,
		OutputPath: outputPath,
		EntryFile:  entryFile,
		ModuleName: info.Name,
		Prefix:     "vmake_plugin_",
	}

	if err := gocompile.CompilePlugin(opts); err != nil {
		return CompileResult{
			PluginDir:  pluginDir,
			PluginName: info.Name,
			OutputPath: outputPath,
			Success:    false,
			Error:      err,
		}
	}

	return CompileResult{
		PluginDir:  pluginDir,
		PluginName: info.Name,
		OutputPath: outputPath,
		Success:    true,
	}
}
