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
	Directory string `json:"directory"`
	Command   string `json:"command"`
	File      string `json:"file"`
}

type CompileCommandsWriter struct {
	commands []CompileCommand
	ccPath   string
	cxxPath  string
	mu       sync.Mutex
}

func NewCompileCommandsWriter(tools *ResolvedTools) *CompileCommandsWriter {
	return &CompileCommandsWriter{
		commands: make([]CompileCommand, 0),
		ccPath:   tools.CC,
		cxxPath:  tools.CXX,
	}
}

func (w *CompileCommandsWriter) AddCommand(dir, src, objPath string, opts *CompileOptions) {
	compiler, flags := selectCompilerAndFlags(w.ccPath, w.cxxPath, opts.CFlags, opts.CxxFlags, opts)

	args := BuildCompileArgs(opts, objPath, src, flags, "")
	cmdStr := iexec.FormatCommandLine(compiler, args)

	w.mu.Lock()
	w.commands = append(w.commands, CompileCommand{
		Directory: dir,
		Command:   cmdStr,
		File:      filepath.Join(dir, src),
	})
	w.mu.Unlock()
}

// Save merges the collected commands into compile_commands.json keyed by
// file: entries from previous runs (or other in-project schedulers, e.g.
// sub-graphs) are kept only for files this writer does not cover. Command
// changes replace their stale entries; sub-graph contributions survive.
func (w *CompileCommandsWriter) Save(outputPath string) error {
	w.mu.Lock()
	commands := append([]CompileCommand{}, w.commands...)
	w.mu.Unlock()

	files := make(map[string]bool, len(commands))
	for _, c := range commands {
		files[c.File] = true
	}

	var existing []CompileCommand
	if _, err := os.Stat(outputPath); err == nil {
		if err := jsonio.Load(outputPath, &existing); err != nil {
			return fmt.Errorf("merge existing %s: %w", outputPath, err)
		}
	}
	merged := make([]CompileCommand, 0, len(existing)+len(commands))
	for _, c := range existing {
		if files[c.File] {
			continue
		}
		merged = append(merged, c)
	}
	merged = append(merged, commands...)

	sort.Slice(merged, func(i, j int) bool {
		if merged[i].File != merged[j].File {
			return merged[i].File < merged[j].File
		}
		return merged[i].Command < merged[j].Command
	})
	return jsonio.Save(outputPath, merged)
}
