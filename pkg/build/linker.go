package build

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/pkg/api"
)

type Linker struct {
	ccPath string
	arPath string
	run    cmdRunner
}

func NewLinker(tools *ResolvedTools) *Linker {
	return &Linker{
		ccPath: tools.CC,
		arPath: tools.AR,
		run:    runGNU,
	}
}

type LinkPolicy struct {
	TargetOS      string
	VersionScript string
	ExcludeLibs   []string
	SymbolBinding string
}

// Validate rejects symbol-management options the target's object format does
// not support. Version scripts, --exclude-libs and -Bsymbolic are ELF-only.
func (p LinkPolicy) Validate() error {
	if p.TargetOS != "windows" {
		return nil
	}
	var unsupported []string
	if p.VersionScript != "" {
		unsupported = append(unsupported, "SetVersionScript")
	}
	if len(p.ExcludeLibs) > 0 {
		unsupported = append(unsupported, "AddExcludeLibs")
	}
	if p.SymbolBinding != "" {
		unsupported = append(unsupported, "SetSymbolBinding")
	}
	if len(unsupported) == 0 {
		return nil
	}
	return fmt.Errorf("%s not supported for PE targets: version scripts, --exclude-libs and -Bsymbolic are ELF-only", strings.Join(unsupported, ", "))
}

// isLibraryArtifact reports whether path is a linkable library rather than a
// relocatable object file. MinGW import libraries (libfoo.dll.a) match the .a
// case because filepath.Ext only sees the final extension.
func isLibraryArtifact(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".a", ".so", ".dylib", ".dll", ".lib":
		return true
	}
	return false
}

// wholeArchiveInput reports whether a linker input belongs inside a
// -Wl,--whole-archive group. A PE DLL and its import library are deliberately
// excluded: whole-archive on an import library force-imports every DLL export,
// defeating --gc-sections and risking duplicate-symbol errors between shared
// deps. Consumers still receive the import library as a normal group input.
func wholeArchiveInput(path string) bool {
	if strings.HasSuffix(strings.ToLower(filepath.Base(path)), ".dll.a") {
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".a", ".so", ".dylib":
		return true
	}
	return false
}

// importLibraryPath returns the MinGW import library LinkShared emits next to
// a PE shared library, or "" for anything else.
func importLibraryPath(outputPath, targetOS string, kind api.TargetKind) string {
	if kind == api.TargetShared && targetOS == "windows" {
		return outputPath + ".a"
	}
	return ""
}

func (p LinkPolicy) versionScriptFlag() string {
	if p.VersionScript == "" {
		return ""
	}
	return "-Wl,--version-script=" + p.VersionScript
}

func (p LinkPolicy) excludeLibsFlag() string {
	if len(p.ExcludeLibs) == 0 {
		return ""
	}
	return "-Wl,--exclude-libs=" + strings.Join(p.ExcludeLibs, ",")
}

func (p LinkPolicy) bindingFlags() []string {
	switch p.SymbolBinding {
	case "static":
		return []string{"-Wl,-Bsymbolic"}
	case "static-functions":
		return []string{"-Wl,-Bsymbolic-functions"}
	default:
		return nil
	}
}

func (l *Linker) LinkBinary(objs, libs, ldflags []string, outputPath, linkerScript string, policy LinkPolicy, workDir string) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	if err := fs.EnsureParentDir(resolveWorkPath(workDir, outputPath)); err != nil {
		return err
	}

	objs = commandPaths(workDir, objs)
	ldflags = commandFlags(workDir, ldflags)
	outputPath = commandPath(workDir, outputPath)
	linkerScript = commandPath(workDir, linkerScript)
	policy.VersionScript = commandPath(workDir, policy.VersionScript)
	args := []string{"-o", outputPath}
	if linkerScript != "" {
		args = append(args, "-T", linkerScript)
	}
	if vs := policy.versionScriptFlag(); vs != "" {
		args = append(args, vs)
	}
	if el := policy.excludeLibsFlag(); el != "" {
		args = append(args, el)
	}

	var objFiles []string
	var libFiles []string
	for _, o := range objs {
		if wholeArchiveInput(o) {
			libFiles = append(libFiles, o)
		} else {
			objFiles = append(objFiles, o)
		}
	}

	var groupFlags []string
	var otherFlags []string
	for _, f := range ldflags {
		if strings.HasPrefix(f, "-l") || strings.HasPrefix(f, "-L") {
			groupFlags = append(groupFlags, f)
		} else {
			otherFlags = append(otherFlags, f)
		}
	}

	if len(objFiles) > 0 || len(libFiles) > 0 || len(libs) > 0 || len(groupFlags) > 0 {
		args = append(args, "-Wl,--start-group")
		args = append(args, objFiles...)
		if len(libFiles) > 0 {
			args = append(args, "-Wl,--whole-archive")
			args = append(args, libFiles...)
			args = append(args, "-Wl,--no-whole-archive")
		}
		for _, lib := range libs {
			args = append(args, "-l"+lib)
		}
		args = append(args, groupFlags...)
		args = append(args, "-Wl,--end-group")
	}

	args = append(args, otherFlags...)
	args = append(args, policy.bindingFlags()...)

	_, err := l.run(l.ccPath, workDir, args...)
	return err
}

func (l *Linker) LinkStatic(objs []string, outputPath, workDir string) error {
	if err := fs.EnsureParentDir(resolveWorkPath(workDir, outputPath)); err != nil {
		return err
	}

	fs.RemoveIfExists(resolveWorkPath(workDir, outputPath))

	objs = commandPaths(workDir, objs)
	args := []string{"rcs", commandPath(workDir, outputPath)}
	args = append(args, objs...)

	_, err := l.run(l.arPath, workDir, args...)
	return err
}

func (l *Linker) LinkShared(objs, ldflags []string, outputPath string, policy LinkPolicy, workDir string) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	if err := fs.EnsureParentDir(resolveWorkPath(workDir, outputPath)); err != nil {
		return err
	}

	objs = commandPaths(workDir, objs)
	ldflags = commandFlags(workDir, ldflags)
	outputPath = commandPath(workDir, outputPath)
	policy.VersionScript = commandPath(workDir, policy.VersionScript)
	filtered := make([]string, 0, len(ldflags))
	for _, f := range ldflags {
		if f == "-pie" || f == "-no-pie" {
			continue
		}
		filtered = append(filtered, f)
	}

	args := []string{"-shared", "-o", outputPath}
	if policy.TargetOS == "windows" {
		// Consumers link against the import library, not the DLL.
		args = append(args, "-Wl,--out-implib="+outputPath+".a")
	}
	if vs := policy.versionScriptFlag(); vs != "" {
		args = append(args, vs)
	}
	if el := policy.excludeLibsFlag(); el != "" {
		args = append(args, el)
	}
	args = append(args, objs...)
	args = append(args, filtered...)
	args = append(args, policy.bindingFlags()...)

	_, err := l.run(l.ccPath, workDir, args...)
	return err
}

func (l *Linker) LinkObject(objs []string, outputPath, workDir string) error {
	if err := fs.EnsureParentDir(resolveWorkPath(workDir, outputPath)); err != nil {
		return err
	}

	objs = commandPaths(workDir, objs)
	args := []string{"-r", "-o", commandPath(workDir, outputPath)}
	args = append(args, objs...)

	_, err := l.run(l.ccPath, workDir, args...)
	return err
}
