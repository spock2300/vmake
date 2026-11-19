package build

import (
	"path/filepath"
	"strings"
)

func sourceLanguage(src string) string {
	switch filepath.Ext(src) {
	case ".s":
		return "asm"
	case ".S":
		return "asm-cpp"
	case ".C":
		return "cxx"
	}
	switch strings.ToLower(filepath.Ext(src)) {
	case ".cc", ".cpp", ".cxx":
		return "cxx"
	}
	return "c"
}

type compileArgParts struct {
	prefix  []string
	options []string
	suffix  []string
}

func (p compileArgParts) args() []string {
	args := append([]string{}, p.prefix...)
	args = append(args, p.options...)
	return append(args, p.suffix...)
}

func BuildCompileArgs(opts *CompileOptions, objPath, src string, flags []string, depPath string) []string {
	return buildCompileArgParts(opts, objPath, src, flags, depPath).args()
}

func buildCompileArgParts(opts *CompileOptions, objPath, src string, flags []string, depPath string) compileArgParts {
	prefix := []string{"-c"}
	if opts.Language == "asm" {
		prefix = append(prefix, "-x", "assembler")
	} else if opts.Language == "asm-cpp" {
		prefix = append(prefix, "-x", "assembler-with-cpp")
		if !opts.clang {
			prefix = append(prefix, "-save-temps=obj")
		}
	}

	if depPath != "" {
		if opts.Language == "asm" {
			prefix = append(prefix, "-Xassembler", "--MD", "-Xassembler", depPath)
		} else {
			prefix = append(prefix, "\u002dMMD", "-MP", "-MF", depPath)
			if opts.Language == "asm-cpp" {
				prefix = append(prefix, "-Xassembler", "--MD", "-Xassembler", depPath+".as")
			}
		}
	}

	prefix = append(prefix, "-o", objPath)
	suffix := []string{}
	if opts.clang && (opts.Language == "asm" || opts.Language == "asm-cpp") {
		suffix = append(suffix, "-fno-integrated-as")
	}
	suffix = append(suffix, src)
	return compileArgParts{prefix: prefix, options: appendCompileOptions(nil, opts, flags), suffix: suffix}
}

func appendCompileOptions(args []string, opts *CompileOptions, flags []string) []string {
	for _, inc := range opts.Includes {
		args = append(args, "-I"+inc)
	}

	if !opts.clang || opts.Language != "asm" {
		for _, def := range opts.Defines {
			args = append(args, "-D"+def)
		}
	}

	scoped := opts.clang && (opts.Language == "asm" || opts.Language == "asm-cpp")
	if scoped {
		args = append(args, "--start-no-unused-arguments")
	}
	args = append(args, flags...)
	if scoped {
		args = append(args, "--end-no-unused-arguments")
	}
	return args
}
