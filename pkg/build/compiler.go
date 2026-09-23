package build

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
)

type cmdRunner func(name, dir string, args ...string) ([]byte, error)

type Compiler struct {
	ccPath   string
	cxxPath  string
	clangCC  bool
	targetOS string
	run      cmdRunner
	env      map[string]string
}

type CompileOptions struct {
	Includes []string
	Defines  []string
	CFlags   []string
	CxxFlags []string
	Language string
	clang    bool
}

func NewCompiler(tools *ResolvedTools) *Compiler {
	return &Compiler{
		ccPath:   tools.CC,
		cxxPath:  tools.CXX,
		clangCC:  tools.isClangCC(),
		targetOS: tools.targetOS,
		env:      tools.env,
	}
}

func selectCompilerAndFlags(ccPath, cxxPath string, cFlags, cxxFlags []string, opts *CompileOptions) (string, []string) {
	if opts.Language == "cxx" {
		return cxxPath, append([]string{}, cxxFlags...)
	}
	return ccPath, append([]string{}, cFlags...)
}

func (c *Compiler) Compile(src, objPath string, opts *CompileOptions, workDir string) ([]string, error) {
	return c.CompileContext(context.Background(), src, objPath, opts, workDir)
}

func (c *Compiler) CompileContext(ctx context.Context, src, objPath string, opts *CompileOptions, workDir string) ([]string, error) {
	compiler := *c
	if compiler.run == nil {
		compiler.run = gnuRunnerContext(ctx, c.env)
	}
	return compiler.compile(src, objPath, opts, workDir)
}

func (c *Compiler) compile(src, objPath string, opts *CompileOptions, workDir string) (deps []string, err error) {
	if err := fs.EnsureDir(filepath.Dir(resolveWorkPath(workDir, objPath))); err != nil {
		return nil, err
	}

	depPath := objPath + ".d"
	defer func() {
		if err != nil {
			err = errors.Join(err, removeCompileFiles(workDir, objPath, depPath, depPath+".as", strings.TrimSuffix(objPath, filepath.Ext(objPath))+".s"))
		}
	}()

	compiler, flags := selectCompilerAndFlags(c.ccPath, c.cxxPath, opts.CFlags, opts.CxxFlags, opts)
	commandOpts := *opts
	commandOpts.clang = c.clangCC
	if commandOpts.clang && (opts.Language == "asm" || opts.Language == "asm-cpp") {
		return c.compileClangAssembly(src, objPath, &commandOpts, flags, workDir)
	}

	args := compileArgs(&commandOpts, objPath, src, flags, depPath, workDir)

	_, err = c.run(compiler, workDir, args...)
	if err != nil {
		return nil, err
	}

	if opts.Language == "asm" || opts.Language == "asm-cpp" {
		depFiles := []string{depPath}
		intermediate := ""
		if opts.Language == "asm-cpp" {
			depFiles = append(depFiles, depPath+".as")
			intermediate = strings.TrimSuffix(objPath, filepath.Ext(objPath)) + ".s"
		}
		return mergeAssemblyDeps(src, objPath, depPath, workDir, intermediate, depFiles...)
	}

	deps, err = ParseDepFile(resolveWorkPath(workDir, depPath))
	if err != nil {
		return nil, fmt.Errorf("failed to parse dep file: %w", err)
	}

	return deps, nil
}

// ParseDepFile parses a gcc/clang -MD style depfile: a "target: deps..."
// rule possibly wrapped across lines with backslash continuations, where
// spaces in paths are escaped as "\ ". Everything after the first ":" up to
// the next rule is the dependency list, from which the first entry (the
// primary source file) is dropped — the scheduler already tracks it as the
// compile input, so only the header dependencies remain. Phony rules
// contribute nothing.
func ParseDepFile(depPath string) ([]string, error) {
	deps, err := readDepInputs(depPath)
	if err != nil {
		return nil, err
	}
	if len(deps) > 0 {
		deps = deps[1:]
	}
	return deps, nil
}

func readDepInputs(depPath string) ([]string, error) {
	data, err := os.ReadFile(depPath)
	if err != nil {
		return nil, err
	}
	content := strings.ReplaceAll(string(data), "\\\r\n", " ")
	content = strings.ReplaceAll(content, "\\\n", " ")
	line, _, _ := strings.Cut(content, "\n")
	tokens := tokenizeDepFile(line)
	for i, tok := range tokens {
		if tok == ":" {
			return tokens[i+1:], nil
		}
	}
	return nil, fmt.Errorf("dependency file %s contains no rule", depPath)
}

func mergeAssemblyDeps(src, objPath, depPath, workDir, intermediate string, depFiles ...string) ([]string, error) {
	var all []string
	for _, file := range depFiles {
		inputs, err := readDepInputs(resolveWorkPath(workDir, file))
		if err != nil {
			return nil, fmt.Errorf("read assembly dependencies %s: %w", file, err)
		}
		all = append(all, inputs...)
	}
	var deps []string
	for _, dep := range unique(all) {
		abs := filepath.Clean(resolveWorkPath(workDir, dep))
		if abs == filepath.Clean(resolveWorkPath(workDir, src)) {
			continue
		}
		if intermediate != "" && abs == filepath.Clean(resolveWorkPath(workDir, intermediate)) {
			continue
		}
		deps = append(deps, dep)
	}
	escape := strings.NewReplacer("\\", "\\\\", " ", "\\ ", "#", "\\#")
	var rule strings.Builder
	rule.WriteString(escape.Replace(objPath) + ": " + escape.Replace(src))
	for _, dep := range deps {
		rule.WriteString(" " + escape.Replace(dep))
	}
	rule.WriteByte('\n')
	if err := os.WriteFile(resolveWorkPath(workDir, depPath), []byte(rule.String()), 0644); err != nil {
		return nil, err
	}
	return deps, nil
}

func tokenizeDepFile(content string) []string {
	var tokens []string
	var cur strings.Builder

	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}

	for i := 0; i < len(content); i++ {
		c := content[i]
		switch {
		case c == '\\':
			// Make-style escapes: a backslash escapes whitespace, '#' and
			// itself. Any other backslash is a literal Windows path separator
			// (C:\dir\file.h), not an escape.
			if i+1 < len(content) && strings.IndexByte(" \t\n\r#\\", content[i+1]) >= 0 {
				next := content[i+1]
				i++
				if next == '\n' || next == '\r' {
					continue
				}
				cur.WriteByte(next)
				continue
			}
			cur.WriteByte('\\')
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			flush()
		case c == ':':
			if i+1 >= len(content) || content[i+1] == ' ' || content[i+1] == '\t' || content[i+1] == '\n' || content[i+1] == '\r' {
				flush()
				tokens = append(tokens, ":")
			} else {
				cur.WriteByte(c)
			}
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return tokens
}
