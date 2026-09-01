package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/fs"
)

type cmdRunner func(name, dir string, args ...string) ([]byte, error)

type Compiler struct {
	ccPath  string
	cxxPath string
	run     cmdRunner
}

type CompileOptions struct {
	Includes []string
	Defines  []string
	CFlags   []string
	CxxFlags []string
	Language string
}

func NewCompiler(tools *ResolvedTools) *Compiler {
	return &Compiler{
		ccPath:  tools.CC,
		cxxPath: tools.CXX,
		run:     iexec.RunInDir,
	}
}

func selectCompilerAndFlags(ccPath, cxxPath string, cFlags, cxxFlags []string, opts *CompileOptions) (string, []string) {
	if opts.Language == "cxx" {
		return cxxPath, append([]string{}, cxxFlags...)
	}
	return ccPath, append([]string{}, cFlags...)
}

func (c *Compiler) Compile(src, objPath string, opts *CompileOptions, workDir string) ([]string, error) {
	if err := fs.EnsureDir(filepath.Dir(resolveWorkPath(workDir, objPath))); err != nil {
		return nil, err
	}

	depPath := objPath + ".d"

	compiler, flags := selectCompilerAndFlags(c.ccPath, c.cxxPath, opts.CFlags, opts.CxxFlags, opts)

	args := BuildCompileArgs(opts, objPath, src, flags, depPath)

	_, err := c.run(compiler, workDir, args...)
	if err != nil {
		return nil, err
	}

	deps, err := ParseDepFile(resolveWorkPath(workDir, depPath))
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
	data, err := os.ReadFile(depPath)
	if err != nil {
		return nil, err
	}

	tokens := tokenizeDepFile(string(data))

	ruleStart := -1
	for i, tok := range tokens {
		if tok == ":" {
			ruleStart = i
			break
		}
	}
	if ruleStart < 0 {
		return nil, nil
	}

	var deps []string
	for _, tok := range tokens[ruleStart+1:] {
		if tok == ":" {
			break
		}
		deps = append(deps, tok)
	}
	if len(deps) > 0 {
		deps = deps[1:]
	}
	return deps, nil
}

func tokenizeDepFile(content string) []string {
	var tokens []string
	var cur strings.Builder
	escaped := false

	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}

	for i := 0; i < len(content); i++ {
		c := content[i]
		switch {
		case escaped:
			cur.WriteByte(c)
			escaped = false
		case c == '\\':
			if i+1 < len(content) && content[i+1] == '\n' {
				i++
				continue
			}
			if i+1 < len(content) && content[i+1] == ' ' {
				cur.WriteByte(' ')
				i++
				continue
			}
			escaped = true
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

func IsSourceValid(src, objPath string, workDir string) (bool, []string) {
	absObj := resolveWorkPath(workDir, objPath)
	objInfo, err := os.Stat(absObj)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, nil
	}

	depPath := objPath + ".d"
	deps, err := ParseDepFile(resolveWorkPath(workDir, depPath))
	if err != nil {
		return false, nil
	}

	objTime := objInfo.ModTime()

	absSrc := resolveWorkPath(workDir, src)
	srcInfo, err := os.Stat(absSrc)
	if err != nil || srcInfo.ModTime().After(objTime) {
		return false, deps
	}

	for _, dep := range deps {
		depInfo, err := os.Stat(resolveWorkPath(workDir, dep))
		if err != nil || depInfo.ModTime().After(objTime) {
			return false, deps
		}
	}

	return true, deps
}
