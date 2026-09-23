package build

import (
	"fmt"
	"os"
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
		run:    gnuRunner(tools.env),
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

func (l *Linker) binaryCommand(objs, libs, ldflags []string, outputPath, linkerScript string, policy LinkPolicy, workDir string) (commandSpec, error) {
	if err := policy.Validate(); err != nil {
		return commandSpec{}, err
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
	var objFiles, libFiles []string
	for _, path := range objs {
		if wholeArchiveInput(path) {
			libFiles = append(libFiles, path)
		} else {
			objFiles = append(objFiles, path)
		}
	}
	var groupFlags, otherFlags []string
	for _, flag := range ldflags {
		if strings.HasPrefix(flag, "-l") || strings.HasPrefix(flag, "-L") {
			groupFlags = append(groupFlags, flag)
		} else {
			otherFlags = append(otherFlags, flag)
		}
	}
	if len(objFiles)+len(libFiles)+len(libs)+len(groupFlags) > 0 {
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
	return commandSpec{Program: l.ccPath, Args: args}, nil
}

func (l *Linker) staticCommand(objs []string, outputPath, workDir string) commandSpec {
	args := []string{"rcs", commandPath(workDir, outputPath)}
	return commandSpec{Program: l.arPath, Args: append(args, commandPaths(workDir, objs)...)}
}

func (l *Linker) sharedCommand(objs, ldflags []string, outputPath string, policy LinkPolicy, workDir string) (commandSpec, error) {
	if err := policy.Validate(); err != nil {
		return commandSpec{}, err
	}
	objs = commandPaths(workDir, objs)
	ldflags = commandFlags(workDir, ldflags)
	outputPath = commandPath(workDir, outputPath)
	policy.VersionScript = commandPath(workDir, policy.VersionScript)
	filtered := make([]string, 0, len(ldflags))
	for _, flag := range ldflags {
		if flag != "-pie" && flag != "-no-pie" {
			filtered = append(filtered, flag)
		}
	}
	args := []string{"-shared", "-o", outputPath}
	if policy.TargetOS == "windows" {
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
	return commandSpec{Program: l.ccPath, Args: args}, nil
}

func (l *Linker) objectCommand(objs []string, outputPath, workDir string) commandSpec {
	args := []string{"-r", "-o", commandPath(workDir, outputPath)}
	return commandSpec{Program: l.ccPath, Args: append(args, commandPaths(workDir, objs)...)}
}

func (l *Linker) execute(command commandSpec, outputPath, workDir string, archive bool) error {
	absolute := resolveWorkPath(workDir, outputPath)
	if err := fs.EnsureParentDir(absolute); err != nil {
		return err
	}
	if archive {
		if err := os.Remove(absolute); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else if info, err := os.Lstat(absolute); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(absolute); err != nil {
			return err
		}
	}
	_, err := l.run(command.Program, workDir, command.Args...)
	return err
}

func (l *Linker) LinkBinary(objs, libs, ldflags []string, outputPath, linkerScript string, policy LinkPolicy, workDir string) error {
	command, err := l.binaryCommand(objs, libs, ldflags, outputPath, linkerScript, policy, workDir)
	if err != nil {
		return err
	}
	return l.execute(command, outputPath, workDir, false)
}

func (l *Linker) LinkStatic(objs []string, outputPath, workDir string) error {
	return l.execute(l.staticCommand(objs, outputPath, workDir), outputPath, workDir, true)
}

func (l *Linker) LinkShared(objs, ldflags []string, outputPath string, policy LinkPolicy, workDir string) error {
	command, err := l.sharedCommand(objs, ldflags, outputPath, policy, workDir)
	if err != nil {
		return err
	}
	return l.execute(command, outputPath, workDir, false)
}

func (l *Linker) LinkObject(objs []string, outputPath, workDir string) error {
	return l.execute(l.objectCommand(objs, outputPath, workDir), outputPath, workDir, false)
}
