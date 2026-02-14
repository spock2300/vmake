package build

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	vlog "gitee.com/spock2300/vmake/pkg/log"
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

func NewCompiler(tc *toolchain.Toolchain) (*Compiler, error) {
	ccPath, err := toolchain.ResolveToolPath(tc.Tools.CC, tc.InstallPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve CC: %w", err)
	}
	cxxPath, err := toolchain.ResolveToolPath(tc.Tools.CXX, tc.InstallPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve CXX: %w", err)
	}

	return &Compiler{
		tc:      tc,
		ccPath:  ccPath,
		cxxPath: cxxPath,
	}, nil
}

func (c *Compiler) Compile(src, objPath string, opts *CompileOptions) ([]string, error) {
	objDir := filepath.Dir(objPath)
	if err := os.MkdirAll(objDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create object directory: %w", err)
	}

	depPath := objPath + ".d"

	var compiler string
	var flags []string

	if opts.Language == "cxx" {
		compiler = c.cxxPath
		flags = append([]string{}, opts.CxxFlags...)
	} else {
		compiler = c.ccPath
		flags = append([]string{}, opts.CFlags...)
	}

	args := c.buildArgs(opts, objPath, depPath, src, flags)
	cmdLine := compiler + " " + strings.Join(args, " ")

	vlog.Debug("  %s", cmdLine)

	cmd := exec.Command(compiler, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s\n%s", cmdLine, string(output))
	}

	deps, err := parseDepFile(depPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dep file: %w", err)
	}

	return deps, nil
}

func (c *Compiler) buildArgs(opts *CompileOptions, objPath, depPath, src string, flags []string) []string {
	args := []string{"-c", "-MMD", "-MP"}

	args = append(args, "-o", objPath)
	args = append(args, "-MF", depPath)

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

func parseDepFile(depPath string) ([]string, error) {
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
