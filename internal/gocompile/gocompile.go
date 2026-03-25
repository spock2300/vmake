package gocompile

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/pkg/version"
)

type PluginOptions struct {
	WorkDir    string
	OutputPath string
	EntryFile  string
	ModuleName string
	Prefix     string // "vmake_plugin_" or "vmake_buildscript_"
}

func CompilePlugin(opts PluginOptions) error {
	vmakeDir := os.Getenv("VMAKE_DIR")
	goModContent := BuildGoModContent(opts, vmakeDir)

	needCleanup := WriteGoModIfMissing(opts.WorkDir, goModContent)

	if err := runGoModTidy(opts.WorkDir); err != nil {
		if needCleanup {
			CleanupGoMod(opts.WorkDir)
		}
		return err
	}

	if err := runGoBuild(opts.WorkDir, opts.OutputPath, opts.EntryFile); err != nil {
		if needCleanup {
			CleanupGoMod(opts.WorkDir)
		}
		return err
	}

	if needCleanup {
		CleanupGoMod(opts.WorkDir)
	}
	return nil
}

func BuildGoModContent(opts PluginOptions, vmakeDir string) string {
	moduleName := opts.Prefix + SanitizeModuleName(opts.ModuleName)
	goVersion := GetCurrentGoVersion()

	if vmakeDir != "" {
		return fmt.Sprintf(`module %s

go %s

require gitee.com/spock2300/vmake v0.0.0

replace gitee.com/spock2300/vmake => %s
`, moduleName, goVersion, vmakeDir)
	}

	vmakeVersion := GetVMakeVersion()
	return fmt.Sprintf(`module %s

go %s

require gitee.com/spock2300/vmake %s
`, moduleName, goVersion, vmakeVersion)
}

func GetVMakeVersion() string {
	v := version.Version
	if v == "dev" {
		return "latest"
	}
	return v
}

func GetCurrentGoVersion() string {
	v := runtime.Version()
	// v is like "go1.22.0" or "go1.26.0"
	// strip "go" prefix
	v = strings.TrimPrefix(v, "go")
	// find first dot after major.minor
	parts := strings.SplitN(v, ".", 3)
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return v
}

func WriteGoModIfMissing(workDir, content string) bool {
	goModPath := filepath.Join(workDir, "go.mod")
	if !fs.FileExists(goModPath) {
		os.WriteFile(goModPath, []byte(content), 0644)
		return true
	}
	return false
}

func CleanupGoMod(workDir string) {
	fs.RemoveIfExists(filepath.Join(workDir, "go.mod"))
	fs.RemoveIfExists(filepath.Join(workDir, "go.sum"))
}

func SanitizeModuleName(name string) string {
	return strings.ReplaceAll(name, "/", "_")
}

func runGoModTidy(workDir string) error {
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go mod tidy failed: %s", string(output))
	}
	return nil
}

func runGoBuild(workDir, outputPath, entryFile string) error {
	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", outputPath, entryFile)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("compilation failed: %s", string(output))
	}
	return nil
}
