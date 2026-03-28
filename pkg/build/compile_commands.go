package build

import (
	"path/filepath"
	"sync"

	iexec "gitee.com/spock2300/vmake/internal/exec"
	"gitee.com/spock2300/vmake/internal/jsonio"
)

type CompileCommand struct {
	Directory string `json:"directory"`
	Command   string `json:"command"`
	File      string `json:"file"`
}

type CompileCommandsWriter struct {
	commands []CompileCommand
	ccPath   string
	cxxPath  string
	pkgDir   string
	mu       sync.Mutex
}

func NewCompileCommandsWriter(tools *ResolvedTools) *CompileCommandsWriter {
	return &CompileCommandsWriter{
		commands: make([]CompileCommand, 0),
		ccPath:   tools.CC,
		cxxPath:  tools.CXX,
	}
}

func (w *CompileCommandsWriter) SetPackageDir(dir string) {
	w.pkgDir = dir
}

func (w *CompileCommandsWriter) AddCommand(src, objPath string, opts *CompileOptions) {
	var compiler string
	var flags []string

	if opts.Language == "cxx" {
		compiler = w.cxxPath
		flags = append([]string{}, opts.CxxFlags...)
	} else {
		compiler = w.ccPath
		flags = append([]string{}, opts.CFlags...)
	}

	args := BuildCompileArgs(opts, objPath, src, flags, "")
	cmdStr := iexec.FormatCommandLine(compiler, args)

	w.mu.Lock()
	w.commands = append(w.commands, CompileCommand{
		Directory: w.pkgDir,
		Command:   cmdStr,
		File:      filepath.Join(w.pkgDir, src),
	})
	w.mu.Unlock()
}

func (w *CompileCommandsWriter) Save(outputPath string) error {
	return jsonio.Save(outputPath, w.commands)
}
