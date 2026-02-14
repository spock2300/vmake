package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gitee.com/spock2300/vmake/pkg/toolchain"
)

type Linker struct {
	tc     *toolchain.Toolchain
	ccPath string
	arPath string
}

func NewLinker(tc *toolchain.Toolchain) (*Linker, error) {
	ccPath, err := toolchain.ResolveToolPath(tc.Tools.CC, tc.InstallPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve CC for linking: %w", err)
	}
	arPath, err := toolchain.ResolveToolPath(tc.Tools.AR, tc.InstallPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve AR: %w", err)
	}

	return &Linker{
		tc:     tc,
		ccPath: ccPath,
		arPath: arPath,
	}, nil
}

func (l *Linker) LinkBinary(objs, libs, ldflags []string, outputPath string) error {
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	args := []string{"-o", outputPath}
	args = append(args, objs...)

	for _, lib := range libs {
		args = append(args, "-l"+lib)
	}

	args = append(args, ldflags...)

	cmd := exec.Command(l.ccPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("link failed: %w\n%s", err, string(output))
	}

	return nil
}

func (l *Linker) LinkStatic(objs []string, outputPath string) error {
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	args := []string{"rcs", outputPath}
	args = append(args, objs...)

	cmd := exec.Command(l.arPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("static library creation failed: %w\n%s", err, string(output))
	}

	return nil
}

func (l *Linker) LinkShared(objs, ldflags []string, outputPath string) error {
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	args := []string{"-shared", "-o", outputPath}
	args = append(args, objs...)
	args = append(args, ldflags...)

	cmd := exec.Command(l.ccPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("shared library creation failed: %w\n%s", err, string(output))
	}

	return nil
}
