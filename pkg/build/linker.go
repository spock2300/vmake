package build

import (
	iexec "gitee.com/spock2300/vmake/internal/exec"
	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/pkg/toolchain"
)

type Linker struct {
	tc     *toolchain.Toolchain
	ccPath string
	arPath string
}

func NewLinker(tc *toolchain.Toolchain, tools *ResolvedTools) *Linker {
	return &Linker{
		tc:     tc,
		ccPath: tools.CC,
		arPath: tools.AR,
	}
}

func (l *Linker) LinkBinary(objs, libs, ldflags []string, outputPath string) error {
	if err := fs.EnsureParentDir(outputPath); err != nil {
		return err
	}

	args := []string{"-o", outputPath}
	args = append(args, objs...)

	for _, lib := range libs {
		args = append(args, "-l"+lib)
	}

	args = append(args, ldflags...)

	_, err := iexec.Run(l.ccPath, args...)
	return err
}

func (l *Linker) LinkStatic(objs []string, outputPath string) error {
	if err := fs.EnsureParentDir(outputPath); err != nil {
		return err
	}

	args := []string{"rcs", outputPath}
	args = append(args, objs...)

	_, err := iexec.Run(l.arPath, args...)
	return err
}

func (l *Linker) LinkShared(objs, ldflags []string, outputPath string) error {
	if err := fs.EnsureParentDir(outputPath); err != nil {
		return err
	}

	args := []string{"-shared", "-o", outputPath}
	args = append(args, objs...)
	args = append(args, ldflags...)

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
