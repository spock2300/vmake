package plugin

import (
	"fmt"
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
		return CompileResult{CompileResult: gocompile.NewFailResult(err), PluginDir: pluginDir}
	}

	entryPath := filepath.Join(pluginDir, info.Entry)
	if !fs.FileExists(entryPath) {
		return CompileResult{CompileResult: gocompile.NewFailResult(fmt.Errorf("entry file not found: %s", entryPath)), PluginDir: pluginDir, PluginName: info.Name}
	}

	outputPath := filepath.Join(pluginDir, "plugin.so")

	if !force && fs.FileExists(outputPath) {
		return CompileResult{CompileResult: gocompile.NewOkResult(outputPath), PluginDir: pluginDir, PluginName: info.Name}
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

	return CompileResult{
		CompileResult: gocompile.CompilePluginToOutput(opts, force),
		PluginDir:     pluginDir,
		PluginName:    info.Name,
	}
}
