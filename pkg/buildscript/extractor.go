package buildscript

import (
	"os"
	"plugin"

	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
)

func ExtractPackage(loaded *LoadedScript) *api.Package {
	if loaded == nil || loaded.Script == nil {
		return nil
	}
	if loaded.pkg != nil {
		return loaded.pkg
	}

	pkg := api.NewPackage()

	if dir := loaded.Source.Dir; dir != "" {
		pkg.SetScriptDir(dir)
	}

	origDir, err := os.Getwd()
	if err != nil {
		vlog.Fatal("get working directory: %v", err)
	}
	defer os.Chdir(origDir)
	if dir := loaded.Source.Dir; dir != "" {
		if err := os.Chdir(dir); err != nil {
			vlog.Fatal("chdir to %s: %v", dir, err)
		}
	}

	if mainFunc := lookupMain(loaded.Script); mainFunc != nil {
		mainFunc(pkg)
	}

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

	loaded.pkg = pkg
	return pkg
}

func lookupMain(p *plugin.Plugin) func(*api.Package) {
	mainSym, err := p.Lookup("Main")
	if err != nil {
		return nil
	}
	mainFunc, ok := mainSym.(func(*api.Package))
	if !ok {
		return nil
	}
	return mainFunc
}
