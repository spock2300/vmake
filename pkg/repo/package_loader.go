package repo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/plugin"
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
	pkgName := extractPackageName(pkgPath)
	pluginPath, err := l.compile(pkgPath, pkgName)
	if err != nil {
		return nil, fmt.Errorf("failed to compile package: %w", err)
	}

	pkg, err := l.loadPlugin(pluginPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load package: %w", err)
	}

	return pkg, nil
}

func (l *PackageLoader) compile(pkgPath, pkgName string) (string, error) {
	vmakeDir := os.Getenv("VMAKE_DIR")
	if vmakeDir != "" {
		return l.compileWithGoModReplace(pkgPath, pkgName, vmakeDir)
	}
	return l.compileWithGoModVersion(pkgPath, pkgName)
}

func (l *PackageLoader) compileWithGoModReplace(pkgPath, pkgName, vmakeDir string) (string, error) {
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

	moduleName := "vmake_package_" + sanitizeModuleName(pkgName)
	goModContent := fmt.Sprintf(`module %s

go 1.22

require gitee.com/spock2300/vmake v0.0.0

replace gitee.com/spock2300/vmake => %s
`, moduleName, vmakeDir)
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

func (l *PackageLoader) compileWithGoModVersion(pkgPath, pkgName string) (string, error) {
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

	moduleName := "vmake_package_" + sanitizeModuleName(pkgName)
	goModContent := fmt.Sprintf(`module %s

go 1.22

require gitee.com/spock2300/vmake %s
`, moduleName, vmakeVersion)
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
	p, err := plugin.GlobalManager.Open(pluginPath)
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

func extractPackageName(pkgPath string) string {
	dir := filepath.Dir(pkgPath)
	parts := strings.Split(dir, string(filepath.Separator))

	for i := len(parts) - 1; i >= 2; i-- {
		if parts[i-2] == "packages" {
			repoIdx := i - 3
			if repoIdx >= 0 {
				repo := parts[repoIdx]
				name := parts[i]
				return repo + "/" + name
			}
		}
	}
	return "unknown"
}

func sanitizeModuleName(name string) string {
	return strings.ReplaceAll(name, "/", "_")
}
