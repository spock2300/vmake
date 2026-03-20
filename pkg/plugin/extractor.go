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

	requireCtx := api.NewRequireContext()
	for _, fn := range builder.GetRequireFuncs() {
		fn(requireCtx)
	}
	for _, req := range requireCtx.GetRequires() {
		pkg.GetRequireContext().AddRequires(req.Name)
	}

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
	pkg.SetInstallFuncs(builder.GetInstallFuncs())

	if len(builder.GetBuildFuncs()) > 0 {
		pkg.SetBuildFuncs(builder.GetBuildFuncs())
		pkg.SetPackageBuildFunc(createPackageBuildFunc(builder))
	}

	loaded.pkg = pkg
	return pkg
}

func createPackageBuildFunc(builder *api.Builder) func(*api.PackageContext) {
	return func(ctx *api.PackageContext) {
		buildCtx := api.NewBuildContext(ctx.PackageName(), ctx.GetConfigValues())
		buildCtx.SetOptions(ctx.GetOptions())

		for _, fn := range builder.GetBuildFuncs() {
			fn(buildCtx)
		}

		for name, t := range buildCtx.GetTargets() {
			pt := ctx.Target(name)
			pt.SetKind(t.Kind())
			pt.AddFiles(toAnySlice(t.Files())...)
			pt.AddIncludes(toAnySlice(t.Includes())...)
			pt.AddPublicIncludes(toAnySlice(t.PublicIncludes())...)
			pt.AddDefines(toAnySlice(t.Defines())...)
			pt.AddCFlags(toAnySlice(t.CFlags())...)
			pt.AddCxxFlags(toAnySlice(t.CxxFlags())...)
			pt.AddLdFlags(toAnySlice(t.LdFlags())...)
			if t.BuildFunc() != nil {
				pt.SetBuildFunc(t.BuildFunc())
			}
		}

		for _, t := range buildCtx.GetTargets() {
			if t.Kind() == api.TargetVoid && t.BuildFunc() != nil {
				t.BuildFunc()(ctx)
			}
		}
	}
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

func toAnySlice(s []string) []any {
	result := make([]any, len(s))
	for i, v := range s {
		result[i] = v
	}
	return result
}
