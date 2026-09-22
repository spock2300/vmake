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

func BuildCompileArgs(opts *CompileOptions, objPath, src string, flags []string, depPath string) []string {
	args := []string{"-c"}
	if opts.Language == "asm" {
		args = append(args, "-x", "assembler")
	} else if opts.Language == "asm-cpp" {
		args = append(args, "-x", "assembler-with-cpp")
		if !opts.clang {
			args = append(args, "-save-temps=obj")
		}
	}

	if depPath != "" {
		if opts.Language == "asm" {
			args = append(args, "-Xassembler", "--MD", "-Xassembler", depPath)
		} else {
			args = append(args, "\u002dMMD", "-MP", "-MF", depPath)
			if opts.Language == "asm-cpp" {
				args = append(args, "-Xassembler", "--MD", "-Xassembler", depPath+".as")
			}
		}
	}

	args = append(args, "-o", objPath)
	args = appendCompileOptions(args, opts, flags)
	if opts.clang && (opts.Language == "asm" || opts.Language == "asm-cpp") {
		args = append(args, "-fno-integrated-as")
	}
	return append(args, src)
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
