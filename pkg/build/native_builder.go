package build

import (
	"fmt"
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/toolchain"
)

type NativeBuilder struct {
	compiler  *Compiler
	linker    *Linker
	tc        *toolchain.Toolchain
	sourceDir string
	buildDir  string
	origDir   string
}

func NewNativeBuilder(tc *toolchain.Toolchain, sourceDir, buildDir string) (*NativeBuilder, error) {
	compiler, err := NewCompiler(tc)
	if err != nil {
		return nil, fmt.Errorf("failed to create compiler: %w", err)
	}

	linker, err := NewLinker(tc)
	if err != nil {
		return nil, fmt.Errorf("failed to create linker: %w", err)
	}

	return &NativeBuilder{
		compiler:  compiler,
		linker:    linker,
		tc:        tc,
		sourceDir: sourceDir,
		buildDir:  buildDir,
	}, nil
}

func (b *NativeBuilder) Build(t *api.Target) error {
	var err error
	b.origDir, err = os.Getwd()
	if err != nil {
		return err
	}

	if err := os.Chdir(b.sourceDir); err != nil {
		return err
	}
	defer os.Chdir(b.origDir)

	files := t.Files()
	if len(files) == 0 {
		return nil
	}

	objs := make([]string, 0, len(files))
	opts := &CompileOptions{
		Includes: t.Includes(),
		Defines:  t.Defines(),
		CFlags:   t.CFlags(),
		CxxFlags: t.CxxFlags(),
		Mode:     "release",
	}

	for _, src := range files {
		objPath := filepath.Join(b.buildDir, src+".o")

		if err := os.MkdirAll(filepath.Dir(objPath), 0755); err != nil {
			return err
		}

		if _, err := b.compiler.Compile(src, objPath, opts); err != nil {
			return fmt.Errorf("failed to compile %s: %w", src, err)
		}

		objs = append(objs, objPath)
	}

	outputPath := filepath.Join(b.buildDir, b.getOutputName(t))

	switch t.Kind() {
	case api.TargetBinary:
		return b.linker.LinkBinary(objs, t.Links(), t.LdFlags(), outputPath)
	case api.TargetStatic:
		return b.linker.LinkStatic(objs, outputPath)
	case api.TargetShared:
		return b.linker.LinkShared(objs, t.LdFlags(), outputPath)
	case api.TargetObject:
		return b.linker.LinkObject(objs, outputPath)
	}

	return nil
}

func (b *NativeBuilder) getOutputName(t *api.Target) string {
	name := t.Name()
	switch t.Kind() {
	case api.TargetBinary:
		return name
	case api.TargetStatic:
		return "lib" + name + ".a"
	case api.TargetShared:
		return "lib" + name + ".so"
	case api.TargetObject:
		return name + ".o"
	}
	return name
}
