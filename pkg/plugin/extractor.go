package plugin

import (
	"plugin"

	"gitee.com/spock2300/vmake/pkg/api"
)

func ExtractPackage(loaded *LoadedPlugin) *api.Package {
	if loaded == nil || loaded.Plugin == nil {
		return nil
	}
	if loaded.pkg != nil {
		return loaded.pkg
	}

	builder := &api.Builder{}
	mainFunc := lookupMain(loaded.Plugin)
	if mainFunc != nil {
		mainFunc(builder)
	}

	pkg := api.NewPackage()

	// Run OnConfig first to collect option definitions (needed by OnRequire discoverAll)
	optDefs := make(map[string]*api.Option)
	for _, fn := range builder.GetConfigFuncs() {
		cfgCtx := api.NewConfigContext("")
		fn(cfgCtx)
		for k, v := range cfgCtx.GetOptions() {
			optDefs[k] = v
		}
	}

	// Phase 1: OnRequire with discoverAll mode (cfgVals=nil, option definitions available)
	requireCtx := api.NewRequireContextForConfig(nil, optDefs, nil)
	for _, fn := range builder.GetRequireFuncs() {
		fn(requireCtx)
	}
	for _, req := range requireCtx.GetRequires() {
		pkg.GetRequireContext().AddRequires(req.Name)
	}
	pkg.SetRequireFuncs(builder.GetRequireFuncs())

	if fn := builder.GetPackageFunc(); fn != nil {
		ctx := api.NewPackageContextForDefinition()
		fn(ctx)

		pkg.SetGit(ctx.GitURLs()...)
		pkg.SetHomepage(ctx.Homepage())
		pkg.SetDescription(ctx.Description())
		pkg.SetLicense(ctx.License())
		pkg.SetLibs(ctx.Libs()...)

		for _, declared := range ctx.DeclaredPackages() {
			pkg.DeclarePackages(declared)
		}

		for ver, ref := range ctx.Versions() {
			pkg.AddVersion(ver, ref)
		}

		for name, opt := range ctx.GetOptions() {
			pkgOpt := pkg.Option(name)
			pkgOpt.SetType(opt.Type()).
				SetDefault(opt.Default()).
				SetDescription(opt.Description())
			if opt.Values() != nil {
				pkgOpt.SetValues(opt.Values()...)
			}
		}
	}

	pkg.SetConfigFuncs(builder.GetConfigFuncs())
	pkg.SetBuildFuncs(builder.GetBuildFuncs())
	pkg.SetInstallFuncs(builder.GetInstallFuncs())

	loaded.pkg = pkg
	return pkg
}

func lookupMain(p *plugin.Plugin) func(*api.Builder) {
	mainSym, err := p.Lookup("Main")
	if err != nil {
		return nil
	}
	mainFunc, ok := mainSym.(func(*api.Builder))
	if !ok {
		return nil
	}
	return mainFunc
}
