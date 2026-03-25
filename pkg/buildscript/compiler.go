package buildscript

import (
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/internal/gocompile"
)

type CompileResult struct {
	Source     Source
	ScriptPath string
	Success    bool
	Error      error
}

func Compile(src Source) CompileResult {
	outputDir := src.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(src.Dir, "build")
	}

	if err := fs.EnsureDir(outputDir); err != nil {
		return CompileResult{
			Source:  src,
			Success: false,
			Error:   err,
		}
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
		return CompileResult{
			Source:     src,
			ScriptPath: scriptPath,
			Success:    false,
			Error:      err,
		}
	}

	return CompileResult{
		Source:     src,
		ScriptPath: scriptPath,
		Success:    true,
	}
}

func CompileAll(sources []Source) []CompileResult {
	results := make([]CompileResult, len(sources))
	for i, src := range sources {
		results[i] = Compile(src)
	}
	return results
}
