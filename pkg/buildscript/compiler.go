package buildscript

import (
	"gitee.com/spock2300/vmake/internal/gocompile"
)

type CompileResult struct {
	gocompile.CompileResult
	Source     Source
	ScriptPath string
}

func Compile(src Source) CompileResult {
	outputDir := src.GetOutputDir()
	scriptPath := outputDir + "/build.so"

	opts := gocompile.PluginOptions{
		WorkDir:    src.Dir,
		OutputPath: scriptPath,
		EntryFile:  "build.go",
		ModuleName: src.Name,
		Prefix:     "vmake_buildscript_",
	}

	return CompileResult{
		CompileResult: gocompile.CompilePluginToOutput(opts, src.Force),
		Source:        src,
		ScriptPath:    scriptPath,
	}
}
