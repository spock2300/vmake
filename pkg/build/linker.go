package build

import (
	"path/filepath"

	iexec "gitee.com/spock2300/vmake/internal/exec"
	"gitee.com/spock2300/vmake/pkg/toolchain"
)

type Linker struct {
	tc     *toolchain.Toolchain
	ccPath string
	arPath string
}

func ensureParentDir(path string) error {
	return ensureDir(filepath.Dir(path))
}

func NewLinker(tc *toolchain.Toolchain) (*Linker, error) {
	tools, err := ResolveTools(tc)
	if err != nil {
		return nil, err
	}

	return &Linker{
		tc:     tc,
		ccPath: tools.CC,
		arPath: tools.AR,
	}, nil
}

func (l *Linker) LinkBinary(objs, libs, ldflags []string, outputPath string) error {
	if err := ensureParentDir(outputPath); err != nil {
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
	if err := ensureParentDir(outputPath); err != nil {
		return err
	}

	args := []string{"rcs", outputPath}
	args = append(args, objs...)

	_, err := iexec.Run(l.arPath, args...)
	return err
}

func (l *Linker) LinkShared(objs, ldflags []string, outputPath string) error {
	if err := ensureParentDir(outputPath); err != nil {
		return err
	}

	args := []string{"-shared", "-o", outputPath}
	args = append(args, objs...)
	args = append(args, ldflags...)

	_, err := iexec.Run(l.ccPath, args...)
	return err
}

func (l *Linker) LinkObject(objs []string, outputPath string) error {
	if err := ensureParentDir(outputPath); err != nil {
		return err
	}

	args := []string{"-r", "-o", outputPath}
	args = append(args, objs...)

	_, err := iexec.Run(l.ccPath, args...)
	return err
}
