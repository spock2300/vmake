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

	builder, err := l.loadPlugin(pluginPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load package: %w", err)
	}

	return l.extractPackageFromBuilder(builder)
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

func (l *PackageLoader) loadPlugin(pluginPath string) (*api.Builder, error) {
	p, err := plugin.GlobalManager.Open(pluginPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open plugin: %w", err)
	}

	mainSym, err := p.Lookup("Main")
	if err != nil {
		return nil, fmt.Errorf("Main function not found: %w", err)
	}

	mainFunc, ok := mainSym.(func(*api.Builder))
	if !ok {
		return nil, fmt.Errorf("Main has wrong type signature")
	}

	builder := &api.Builder{}
	mainFunc(builder)
	return builder, nil
}

func (l *PackageLoader) extractPackageFromBuilder(builder *api.Builder) (*api.Package, error) {
	pkg := api.NewPackage()

	requireCtx := api.NewRequireContext()
	for _, fn := range builder.GetRequireFuncs() {
		fn(requireCtx)
	}
	for _, req := range requireCtx.GetRequires() {
		pkg.GetRequireContext().AddRequires(req.Name)
	}

	if fn := builder.GetPackageFunc(); fn != nil {
		ctx := api.NewPackageContextForDefinition()
		fn(ctx)

		pkg.SetGit(ctx.GitURLs()...)
		pkg.SetHomepage(ctx.Homepage())
		pkg.SetDescription(ctx.Description())
		pkg.SetLicense(ctx.License())
		pkg.SetLibs(ctx.Libs()...)

		for _, declared := range ctx.DeclaredPackages() {
			pkg.DeclarePackages(declared)
		}

		for ver, ref := range ctx.Versions() {
			pkg.AddVersion(ver, ref)
		}

		for name, opt := range ctx.GetOptions() {
			pkgOpt := pkg.Option(name)
			pkgOpt.SetType(opt.Type()).
				SetDefault(opt.Default()).
				SetDescription(opt.Description())
			if opt.Values() != nil {
				pkgOpt.SetValues(opt.Values()...)
			}
		}
	}

	if len(builder.GetBuildFuncs()) > 0 {
		pkg.Build(l.createBuildFunc(builder))
	}

	return pkg, nil
}

func (l *PackageLoader) createBuildFunc(builder *api.Builder) func(ctx *api.PackageContext) {
	return func(ctx *api.PackageContext) {
		buildCtx := api.NewBuildContext(ctx.PackageName(), ctx.GetConfigValues())
		buildCtx.SetOptions(ctx.GetOptions())

		for _, fn := range builder.GetBuildFuncs() {
			fn(buildCtx)
		}

		for name, t := range buildCtx.GetTargets() {
			pt := ctx.Target(name)
			pt.SetKind(t.Kind())
			pt.AddFiles(toAnySlice(t.Files())...)
			pt.AddIncludes(toAnySlice(t.Includes())...)
			pt.AddPublicIncludes(toAnySlice(t.PublicIncludes())...)
			pt.AddDefines(toAnySlice(t.Defines())...)
			pt.AddCFlags(toAnySlice(t.CFlags())...)
			pt.AddCxxFlags(toAnySlice(t.CxxFlags())...)
			pt.AddLdFlags(toAnySlice(t.LdFlags())...)
			if t.BuildFunc() != nil {
				pt.SetBuildFunc(t.BuildFunc())
			}
		}

		for _, t := range buildCtx.GetTargets() {
			if t.Kind() == api.TargetVoid && t.BuildFunc() != nil {
				t.BuildFunc()(ctx)
			}
		}
	}
}

func toAnySlice(s []string) []any {
	result := make([]any, len(s))
	for i, v := range s {
		result[i] = v
	}
	return result
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
