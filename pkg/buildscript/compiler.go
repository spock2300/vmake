package buildscript

import (
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/internal/gocompile"
)

type CompileResult struct {
	gocompile.CompileResult
	Source     Source
	ScriptPath string
}

func Compile(src Source) CompileResult {
	outputDir := src.GetOutputDir()

	if err := fs.EnsureDir(outputDir); err != nil {
		return CompileResult{CompileResult: gocompile.NewFailResult(err), Source: src}
	}

	scriptPath := filepath.Join(outputDir, "build.so")

	if src.Force {
		os.Remove(scriptPath)
	}

	opts := gocompile.PluginOptions{
		WorkDir:    src.Dir,
		OutputPath: scriptPath,
		EntryFile:  "build.go",
		ModuleName: src.Name,
		Prefix:     "vmake_buildscript_",
	}

	if err := gocompile.CompilePlugin(opts); err != nil {
		return CompileResult{CompileResult: gocompile.NewFailResultAt(err, scriptPath), Source: src, ScriptPath: scriptPath}
	}

	return CompileResult{CompileResult: gocompile.NewOkResult(scriptPath), Source: src, ScriptPath: scriptPath}
}
