package build

import (
	"path/filepath"
	"strings"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/fs"
)

type Linker struct {
	ccPath string
	arPath string
}

func NewLinker(tools *ResolvedTools) *Linker {
	return &Linker{
		ccPath: tools.CC,
		arPath: tools.AR,
	}
}

type LinkPolicy struct {
	VersionScript string
	ExcludeLibs   []string
	SymbolBinding string
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

func (l *Linker) LinkBinary(objs, libs, ldflags []string, outputPath, linkerScript string, policy LinkPolicy) error {
	if err := fs.EnsureParentDir(outputPath); err != nil {
		return err
	}

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
		ext := strings.ToLower(filepath.Ext(o))
		if ext == ".a" || ext == ".so" || ext == ".dylib" {
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

	_, err := iexec.Run(l.ccPath, args...)
	return err
}

func (l *Linker) LinkStatic(objs []string, outputPath string) error {
	if err := fs.EnsureParentDir(outputPath); err != nil {
		return err
	}

	fs.RemoveIfExists(outputPath)

	args := []string{"rcs", outputPath}
	args = append(args, objs...)

	_, err := iexec.Run(l.arPath, args...)
	return err
}

func (l *Linker) LinkShared(objs, ldflags []string, outputPath string, policy LinkPolicy) error {
	if err := fs.EnsureParentDir(outputPath); err != nil {
		return err
	}

	filtered := make([]string, 0, len(ldflags))
	for _, f := range ldflags {
		if f == "-pie" || f == "-no-pie" {
			continue
		}
		filtered = append(filtered, f)
	}

	args := []string{"-shared", "-o", outputPath}
	if vs := policy.versionScriptFlag(); vs != "" {
		args = append(args, vs)
	}
	if el := policy.excludeLibsFlag(); el != "" {
		args = append(args, el)
	}
	args = append(args, objs...)
	args = append(args, filtered...)
	args = append(args, policy.bindingFlags()...)

	_, err := iexec.Run(l.ccPath, args...)
	return err
}

func (l *Linker) LinkObject(objs []string, outputPath string) error {
	if err := fs.EnsureParentDir(outputPath); err != nil {
		return err
	}

	args := []string{"-r", "-o", outputPath}
	args = append(args, objs...)

	_, err := iexec.Run(l.ccPath, args...)
	return err
}
