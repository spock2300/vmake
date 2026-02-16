package repo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/version"
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
		return l.compileWithGoModReplace(pkgPath, vmakeDir)
	}
	return l.compileWithGoModVersion(pkgPath)
}

func (l *PackageLoader) compileWithGoModReplace(pkgPath, vmakeDir string) (string, error) {
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

func (l *PackageLoader) compileWithGoModVersion(pkgPath string) (string, error) {
	vmakeVersion := version.Version
	if vmakeVersion == "dev" {
		vmakeVersion = "latest"
	}

	if err := os.MkdirAll(l.goPath, 0755); err != nil {
		return "", err
	}

	hash := hashPath(pkgPath + vmakeVersion)
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

require gitee.com/spock2300/vmake %s
`, vmakeVersion)
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
