package buildscript

import (
	"fmt"
	"os"

	"github.com/traefik/yaegi/interp"

	"github.com/spock2300/vmake/internal/gosrc"
	"github.com/spock2300/vmake/internal/yaegibase"
	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func yaegiExports() interp.Exports {
	return interp.Exports{
		"github.com/spock2300/vmake/pkg/api/api":             api.YaegiSymbols(),
		"github.com/spock2300/vmake/pkg/toolchain/toolchain": toolchain.YaegiSymbols(),
	}
}

func LoadBuildScript(src Source) (*api.Package, error) {
	i, err := yaegibase.New(yaegiExports())
	if err != nil {
		return nil, err
	}

	merged, err := gosrc.MergeGoSources(src.Dir)
	if err != nil {
		return nil, fmt.Errorf("merge go files in %s: %w", src.Dir, err)
	}

	if _, err := i.Eval(merged); err != nil {
		return nil, fmt.Errorf("yaegi eval %s: %w", src.Name, err)
	}

	v, err := i.Eval("Main")
	if err != nil {
		return nil, fmt.Errorf("yaegi lookup Main in %s: %w", src.Name, err)
	}
	mainFunc, ok := v.Interface().(func(*api.Package))
	if !ok {
		return nil, fmt.Errorf("yaegi: Main in %s has wrong signature: %T", src.Name, v.Interface())
	}

	pkg := api.NewPackage()
	if dir := src.Dir; dir != "" {
		pkg.SetScriptDir(dir)
	}

	origDir, err := os.Getwd()
	if err != nil {
		vlog.Fatal("get working directory: %v", err)
	}
	defer os.Chdir(origDir)
	if dir := src.Dir; dir != "" {
		if err := os.Chdir(dir); err != nil {
			vlog.Fatal("chdir to %s: %v", dir, err)
		}
	}

	mainFunc(pkg)

	if fn := pkg.GetPackageFunc(); fn != nil {
		fn(pkg)
	}

	if len(pkg.GetRequireFuncs()) > 0 {
		ctx := api.NewRequireContextForConfig(nil, pkg.Options, pkg.GetRequireFuncs())
		for _, fn := range pkg.GetRequireFuncs() {
			fn(ctx)
		}
		pkg.GetRequires().AddInfos(ctx.GetRequires()...)
	}

	return pkg, nil
}
