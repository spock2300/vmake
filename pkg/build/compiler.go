package build

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type Compiler struct {
	ccPath  string
	cxxPath string
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
	}
}

func selectCompilerAndFlags(ccPath, cxxPath string, globalCFlags, globalCxxFlags, cFlags, cxxFlags []string, opts *CompileOptions) (string, []string) {
	if opts.Language == "cxx" {
		return cxxPath, append(append([]string{}, globalCxxFlags...), cxxFlags...)
	}
	return ccPath, append(append([]string{}, globalCFlags...), cFlags...)
}

func (c *Compiler) Compile(src, objPath string, opts *CompileOptions, workDir string) ([]string, error) {
	if err := fs.EnsureDir(filepath.Dir(resolveWorkPath(workDir, objPath))); err != nil {
		return nil, err
	}

	depPath := objPath + ".d"

	mgr := toolchain.GetManager()
	compiler, flags := selectCompilerAndFlags(c.ccPath, c.cxxPath, mgr.GetGlobalCFlags(), mgr.GetGlobalCxxFlags(), opts.CFlags, opts.CxxFlags, opts)

	args := BuildCompileArgs(opts, objPath, src, flags, depPath)

	_, err := iexec.RunInDir(compiler, workDir, args...)
	if err != nil {
		return nil, err
	}

	deps, err := ParseDepFile(resolveWorkPath(workDir, depPath))
	if err != nil {
		return nil, fmt.Errorf("failed to parse dep file: %w", err)
	}

	return deps, nil
}

func ParseDepFile(depPath string) ([]string, error) {
	file, err := os.Open(depPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var deps []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		if strings.HasSuffix(line, ":") {
			continue
		}

		line = strings.TrimSuffix(line, "\\")
		line = strings.TrimSpace(line)

		parts := strings.Fields(line)
		for _, part := range parts {
			if strings.HasSuffix(part, ":") {
				continue
			}
			deps = append(deps, part)
		}
	}

	if len(deps) > 0 {
		deps = deps[1:]
	}

	return deps, scanner.Err()
}

func IsSourceValid(src, objPath string, extraDeps []string, workDir string) (bool, []string) {
	absObj := resolveWorkPath(workDir, objPath)
	objInfo, err := os.Stat(absObj)
	if os.IsNotExist(err) {
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
		depInfo, err := os.Stat(dep)
		if err != nil || depInfo.ModTime().After(objTime) {
			return false, deps
		}
	}

	for _, dep := range extraDeps {
		depPath := resolveWorkPath(workDir, dep)
		depInfo, err := os.Stat(depPath)
		if err == nil && depInfo.ModTime().After(objTime) {
			return false, deps
		}
	}

	return true, deps
}
