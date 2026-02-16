package repo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
)

type PackageLoader struct {
	cacheDir string
	goPath   string
	vmakeDir string
}

func NewPackageLoader(cacheDir string) *PackageLoader {
	return &PackageLoader{
		cacheDir: cacheDir,
		goPath:   filepath.Join(cacheDir, "plugins"),
	}
}

func (l *PackageLoader) SetVMakeDir(dir string) {
	l.vmakeDir = dir
}

func (l *PackageLoader) Load(pkgPath string) (*api.Package, error) {
	pluginPath, err := l.compile(pkgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to compile package: %w", err)
	}

	pkg, err := l.loadPlugin(pluginPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load package: %w", err)
	}

	return pkg, nil
}

func (l *PackageLoader) compile(pkgPath string) (string, error) {
	vmakeDir := os.Getenv("VMAKE_DIR")
	if vmakeDir != "" {
		return l.compileWithGoMod(pkgPath, vmakeDir)
	}
	return l.compileWithGopath(pkgPath)
}

func (l *PackageLoader) compileWithGoMod(pkgPath, vmakeDir string) (string, error) {
	if err := os.MkdirAll(l.goPath, 0755); err != nil {
		return "", err
	}

	hash := hashPath(pkgPath + vmakeDir)
	buildDir := filepath.Join(l.goPath, hash)
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return "", err
	}

	srcContent, err := os.ReadFile(pkgPath)
	if err != nil {
		return "", fmt.Errorf("failed to read package source: %w", err)
	}

	if err := os.WriteFile(filepath.Join(buildDir, "package.go"), srcContent, 0644); err != nil {
		return "", fmt.Errorf("failed to copy package source: %w", err)
	}

	goModContent := fmt.Sprintf(`module vmake_package

go 1.22

require gitee.com/spock2300/vmake v0.0.0

replace gitee.com/spock2300/vmake => %s
`, vmakeDir)
	if err := os.WriteFile(filepath.Join(buildDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		return "", fmt.Errorf("failed to create go.mod: %w", err)
	}

	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = buildDir
	tidyCmd.Env = append(os.Environ(), "GO111MODULE=on")
	if tidyOutput, err := tidyCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go mod tidy error: %s", string(tidyOutput))
	}

	pluginPath := filepath.Join(buildDir, "plugin.so")
	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", "plugin.so", ".")
	cmd.Dir = buildDir
	cmd.Env = append(os.Environ(), "GO111MODULE=on")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("compile error: %s", string(output))
	}

	return pluginPath, nil
}

func (l *PackageLoader) compileWithGopath(pkgPath string) (string, error) {
	vmakePath, err := l.getVMakePath()
	if err != nil {
		return "", err
	}

	userGopath, err := l.getUserGopath()
	if err != nil {
		return "", err
	}

	srcDir := filepath.Join(userGopath, "src", "gitee.com", "spock2300")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		return "", err
	}

	linkPath := filepath.Join(srcDir, "vmake")
	linkTarget, _ := os.Readlink(linkPath)
	if linkTarget != vmakePath {
		os.Remove(linkPath)
		if err := os.Symlink(vmakePath, linkPath); err != nil {
			return "", err
		}
	}

	if err := os.MkdirAll(l.goPath, 0755); err != nil {
		return "", err
	}

	hash := hashPath(pkgPath + vmakePath)
	buildDir := filepath.Join(l.goPath, hash)
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return "", err
	}

	pluginPath := filepath.Join(buildDir, "plugin.so")
	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", "plugin.so", pkgPath)
	cmd.Dir = buildDir
	cmd.Env = append(os.Environ(),
		"GO111MODULE=off",
		"GOPATH="+userGopath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("compile error: %s", string(output))
	}

	return pluginPath, nil
}

func (l *PackageLoader) getVMakePath() (string, error) {
	if l.vmakeDir != "" {
		return l.vmakeDir, nil
	}
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "gitee.com/spock2300/vmake")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("vmake module not found (run 'go install gitee.com/spock2300/vmake/cmd/vmake@latest' or set VMAKE_DIR): %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (l *PackageLoader) getUserGopath() (string, error) {
	cmd := exec.Command("go", "env", "GOPATH")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get GOPATH: %w", err)
	}
	gopath := strings.TrimSpace(string(output))
	if gopath == "" {
		return "", fmt.Errorf("GOPATH is not set")
	}
	if idx := strings.Index(gopath, string(os.PathListSeparator)); idx > 0 {
		gopath = gopath[:idx]
	}
	return gopath, nil
}

func (l *PackageLoader) loadPlugin(pluginPath string) (*api.Package, error) {
	p, err := plugin.Open(pluginPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open plugin: %w", err)
	}

	pkgSym, err := p.Lookup("Package")
	if err != nil {
		return nil, fmt.Errorf("Package function not found: %w", err)
	}

	pkgFunc, ok := pkgSym.(func(*api.Package))
	if !ok {
		return nil, fmt.Errorf("Package has wrong type signature")
	}

	pkg := api.NewPackage()
	pkgFunc(pkg)

	return pkg, nil
}

func hashPath(path string) string {
	hash := uint32(0)
	for _, c := range path {
		hash = hash*31 + uint32(c)
	}
	return fmt.Sprintf("%x", hash)
}
