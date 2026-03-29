package buildscript

import (
	"os"
	"plugin"

	"gitee.com/spock2300/vmake/pkg/api"
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

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	if dir := loaded.Source.Dir; dir != "" {
		os.Chdir(dir)
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
		for _, req := range ctx.GetRequires() {
			pkg.GetRequires().Add(req.Name)
		}
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
