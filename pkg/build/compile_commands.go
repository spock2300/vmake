package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/pkg/toolchain"
)

type CompileCommand struct {
	Directory string `json:"directory"`
	Command   string `json:"command"`
	File      string `json:"file"`
}

type CompileCommandsWriter struct {
	commands []CompileCommand
	tc       *toolchain.Toolchain
	ccPath   string
	cxxPath  string
	pkgDir   string
}

func NewCompileCommandsWriter(tc *toolchain.Toolchain) (*CompileCommandsWriter, error) {
	ccPath, err := toolchain.ResolveToolPath(tc.Tools.CC, tc.InstallPath)
	if err != nil {
		return nil, err
	}
	cxxPath, err := toolchain.ResolveToolPath(tc.Tools.CXX, tc.InstallPath)
	if err != nil {
		return nil, err
	}

	return &CompileCommandsWriter{
		commands: make([]CompileCommand, 0),
		tc:       tc,
		ccPath:   ccPath,
		cxxPath:  cxxPath,
	}, nil
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

	args := w.buildArgs(opts, objPath, src, flags)
	cmdStr := compiler + " " + joinArgs(args)

	w.commands = append(w.commands, CompileCommand{
		Directory: w.pkgDir,
		Command:   cmdStr,
		File:      filepath.Join(w.pkgDir, src),
	})
}

func (w *CompileCommandsWriter) buildArgs(opts *CompileOptions, objPath, src string, flags []string) []string {
	args := []string{"-c"}

	args = append(args, "-o", objPath)

	for _, inc := range opts.Includes {
		args = append(args, "-I"+inc)
	}

	for _, def := range opts.Defines {
		args = append(args, "-D"+def)
	}

	args = append(args, flags...)
	args = append(args, src)

	return args
}

func (w *CompileCommandsWriter) Save(outputPath string) error {
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory for compile_commands.json: %w", err)
	}

	data, err := json.MarshalIndent(w.commands, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal compile_commands.json: %w", err)
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write compile_commands.json: %w", err)
	}

	return nil
}

func joinArgs(args []string) string {
	result := ""
	for i, arg := range args {
		if i > 0 {
			result += " "
		}
		if needsQuoting(arg) {
			result += fmt.Sprintf("\"%s\"", arg)
		} else {
			result += arg
		}
	}
	return result
}

func needsQuoting(s string) bool {
	for _, c := range s {
		if c == ' ' || c == '"' || c == '\\' {
			return true
		}
	}
	return false
}
