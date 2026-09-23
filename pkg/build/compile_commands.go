package build

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/jsonio"
)

type CompileCommand struct {
	Directory string   `json:"directory"`
	Command   string   `json:"command,omitempty"`
	Arguments []string `json:"arguments"`
	File      string   `json:"file"`
}

type CompileCommandsWriter struct {
	commands []CompileCommand
	ccPath   string
	cxxPath  string
	clangCC  bool
	mu       sync.Mutex
}

func NewCompileCommandsWriter(tools *ResolvedTools) *CompileCommandsWriter {
	return &CompileCommandsWriter{
		commands: make([]CompileCommand, 0),
		ccPath:   tools.CC,
		cxxPath:  tools.CXX,
		clangCC:  tools.isClangCC(),
	}
}

func (w *CompileCommandsWriter) AddCommand(dir, src, objPath string, opts *CompileOptions) {
	compiler, flags := selectCompilerAndFlags(w.ccPath, w.cxxPath, opts.CFlags, opts.CxxFlags, opts)

	commandOpts := *opts
	commandOpts.clang = w.clangCC
	args := compileArgs(&commandOpts, objPath, src, flags, "", dir)
	cmdStr := iexec.FormatCommandLine(compiler, args)

	w.mu.Lock()
	w.commands = append(w.commands, CompileCommand{
		Directory: dir,
		Command:   cmdStr,
		Arguments: append([]string{compiler}, args...),
		File:      resolveWorkPath(dir, src),
	})
	w.mu.Unlock()
}

func (w *CompileCommandsWriter) Save(outputPath string) error {
	w.mu.Lock()
	commands := append([]CompileCommand{}, w.commands...)
	w.mu.Unlock()

	var existing []CompileCommand
	if _, err := os.Stat(outputPath); err == nil {
		if err := jsonio.Load(outputPath, &existing); err != nil {
			return fmt.Errorf("merge existing %s: %w", outputPath, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	byOutput := make(map[[2]string]CompileCommand, len(existing)+len(commands))
	for _, c := range append(existing, commands...) {
		output := ""
		for i := 0; i+1 < len(c.Arguments); i++ {
			if c.Arguments[i] == "-o" {
				output = c.Arguments[i+1]
			}
		}
		if output == "" {
			return fmt.Errorf("compile command for %s has no output argument", c.File)
		}
		key := [2]string{filepath.Clean(resolveWorkPath(c.Directory, c.File)), filepath.Clean(resolveWorkPath(c.Directory, output))}
		byOutput[key] = c
	}
	merged := make([]CompileCommand, 0, len(byOutput))
	for _, c := range byOutput {
		merged = append(merged, c)
	}

	sort.Slice(merged, func(i, j int) bool {
		if merged[i].File != merged[j].File {
			return merged[i].File < merged[j].File
		}
		return merged[i].Command < merged[j].Command
	})
	return jsonio.Save(outputPath, merged)
}
