package plugin

import (
	"fmt"
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/internal/gocompile"
)

type CompileResult struct {
	gocompile.CompileResult
	PluginDir  string
	PluginName string
}

func Compile(pluginDir string, force bool) CompileResult {
	info, err := LoadPluginInfo(pluginDir)
	if err != nil {
		return CompileResult{
			CompileResult: gocompile.CompileResult{Success: false, Error: err},
			PluginDir:     pluginDir,
		}
	}

	entryPath := filepath.Join(pluginDir, info.Entry)
	if !fs.FileExists(entryPath) {
		return CompileResult{
			CompileResult: gocompile.CompileResult{Success: false, Error: fmt.Errorf("entry file not found: %s", entryPath)},
			PluginDir:     pluginDir,
			PluginName:    info.Name,
		}
	}

	outputPath := filepath.Join(pluginDir, "plugin.so")

	if force {
		os.Remove(outputPath)
	}

	if fs.FileExists(outputPath) {
		return CompileResult{
			CompileResult: gocompile.CompileResult{Success: true, OutputPath: outputPath},
			PluginDir:     pluginDir,
			PluginName:    info.Name,
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
			CompileResult: gocompile.CompileResult{Success: false, Error: err, OutputPath: outputPath},
			PluginDir:     pluginDir,
			PluginName:    info.Name,
		}
	}

	return CompileResult{
		CompileResult: gocompile.CompileResult{Success: true, OutputPath: outputPath},
		PluginDir:     pluginDir,
		PluginName:    info.Name,
	}
}
