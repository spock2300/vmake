package plugin

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/gocompile"
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

	if !force && fs.FileExists(outputPath) && !isStale(outputPath, entryPath) {
		return CompileResult{CompileResult: gocompile.NewOkResult(outputPath), PluginDir: pluginDir, PluginName: info.Name}
	}

	entryFile := filepath.Base(entryPath)
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

func isStale(soPath, srcPath string) bool {
	soStat, err := os.Stat(soPath)
	if err != nil || soStat.Size() == 0 {
		return true
	}
	if exe, err := os.Executable(); err == nil {
		if exeStat, err := os.Stat(exe); err == nil {
			if exeStat.ModTime().After(soStat.ModTime()) {
				return true
			}
		}
	}
	if srcStat, err := os.Stat(srcPath); err == nil {
		if srcStat.ModTime().After(soStat.ModTime()) {
			return true
		}
	}
	return false
}
