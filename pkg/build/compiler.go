package build

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	iexec "gitee.com/spock2300/vmake/internal/exec"
	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/pkg/toolchain"
)

type Compiler struct {
	tc      *toolchain.Toolchain
	ccPath  string
	cxxPath string
}

type CompileOptions struct {
	Includes []string
	Defines  []string
	CFlags   []string
	CxxFlags []string
	Language string
	Mode     string
}

func NewCompiler(tc *toolchain.Toolchain, tools *ResolvedTools) *Compiler {
	return &Compiler{
		tc:      tc,
		ccPath:  tools.CC,
		cxxPath: tools.CXX,
	}
}

func (c *Compiler) Compile(src, objPath string, opts *CompileOptions) ([]string, error) {
	if err := fs.EnsureDir(filepath.Dir(objPath)); err != nil {
		return nil, err
	}

	depPath := objPath + ".d"

	var compiler string
	var flags []string

	mgr := toolchain.GetManager()
	if opts.Language == "cxx" {
		compiler = c.cxxPath
		flags = append(mgr.GetGlobalCxxFlags(), opts.CxxFlags...)
	} else {
		compiler = c.ccPath
		flags = append(mgr.GetGlobalCFlags(), opts.CFlags...)
	}

	args := BuildCompileArgs(opts, objPath, src, flags, depPath)

	_, err := iexec.Run(compiler, args...)
	if err != nil {
		return nil, err
	}

	deps, err := ParseDepFile(depPath)
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

func IsSourceValid(src, objPath string) (bool, []string) {
	objInfo, err := os.Stat(objPath)
	if os.IsNotExist(err) {
		return false, nil
	}

	depPath := objPath + ".d"
	deps, err := ParseDepFile(depPath)
	if err != nil {
		return false, nil
	}

	srcInfo, err := os.Stat(src)
	if err != nil || srcInfo.ModTime().After(objInfo.ModTime()) {
		return false, deps
	}

	for _, dep := range deps {
		depInfo, err := os.Stat(dep)
		if err != nil || depInfo.ModTime().After(objInfo.ModTime()) {
			return false, deps
		}
	}

	return true, deps
}
