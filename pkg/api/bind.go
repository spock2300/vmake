package api

func BindSetConfigLookup(accessor *ConfigAccessor, fn func(pkg, name string) (any, *Option, error)) {
	accessor.setConfigLookup(fn)
}

func BindSetGlobalCFlagsFunc(ctx *ConfigContext, fn func(...string)) {
	ctx.setGlobalCFlagsFunc(fn)
}

func BindSetGlobalCxxFlagsFunc(ctx *ConfigContext, fn func(...string)) {
	ctx.setGlobalCxxFlagsFunc(fn)
}

func BindSetGlobalLdFlagsFunc(ctx *ConfigContext, fn func(...string)) {
	ctx.setGlobalLdFlagsFunc(fn)
}

func BindSetGlobalLinksFunc(ctx *ConfigContext, fn func(...string)) {
	ctx.setGlobalLinksFunc(fn)
}

func BindSetLenient(ctx *ConfigContext) *ConfigContext {
	return ctx.setLenient()
}

func BindSetOptionExists(ctx *DepConfigContext, fn func(depPkg, option string) (bool, error)) *DepConfigContext {
	return ctx.setOptionExists(fn)
}

func BindSetBuildSubGraphFunc(ctx *BuildContext, fn func(string) error) *BuildContext {
	return ctx.setBuildSubGraphFunc(fn)
}

func BindSetDepOutputFunc(ctx *BuildContext, fn func(string) string) {
	ctx.setDepOutputFunc(fn)
}

func BindSetDepSrcDirFunc(ctx *BuildContext, fn func(string) string) {
	ctx.setDepSrcDirFunc(fn)
}

func BindSetDepPackageDirFunc(ctx *BuildContext, fn func(string) string) {
	ctx.setDepPackageDirFunc(fn)
}

func BindSetPackage(ctx *BuildContext, pkg *Package) *BuildContext {
	return ctx.setPackage(pkg)
}

func BindSetCleanPackage(ctx *CleanContext, pkg *Package) *CleanContext {
	return ctx.setPackage(pkg)
}

func BindSubGraphs(ctx *BuildContext) []string {
	return ctx.hostSubGraphs()
}

func BindBuildContextExecutedHelperStages(ctx *BuildContext) int {
	return ctx.hostExecutedHelperStages()
}

func BindRequireContextGetRequireFuncs(ctx *RequireContext) []RequireFunc {
	return ctx.getRequireFuncs()
}

func BindPackageExecutedHelperStages(p *Package) int {
	return p.hostExecutedHelperStages()
}

func BindPackageExecBuildFuncs(p *Package, dir string, fn func(BuildFunc)) {
	p.hostExecBuildFuncs(dir, fn)
}

func BindPackageExecCleanFuncs(p *Package, dir string, fn func(CleanFunc)) {
	p.hostExecCleanFuncs(dir, fn)
}

func BindPackageExecConfigFuncs(p *Package, dir string, fn func(ConfigFunc)) {
	p.hostExecConfigFuncs(dir, fn)
}

func BindPackageExecConfigureDepsFuncs(p *Package, dir string, fn func(ConfigureDepsFunc)) {
	p.hostExecConfigureDepsFuncs(dir, fn)
}

func BindPackageExecInstallFuncs(p *Package, dir string, fn func(InstallFunc)) {
	p.hostExecInstallFuncs(dir, fn)
}

func BindPackageGeneratedFiles(p *Package) []string { return p.hostGeneratedFiles() }

func BindPackageGetPackageFunc(p *Package) PackageFunc { return p.hostGetPackageFunc() }

func BindPackageGetRequireFuncs(p *Package) []RequireFunc { return p.hostGetRequireFuncs() }

func BindPackageHasBuildFuncs(p *Package) bool { return p.hostHasBuildFuncs() }

func BindPackageHasConfigureDepsFuncs(p *Package) bool { return p.hostHasConfigureDepsFuncs() }

func BindPackageHasGenerators(p *Package) bool { return p.hostHasGenerators() }

func BindPackageResetApplyState(p *Package) { p.hostResetApplyState() }

func BindPackageRunGenerators(p *Package, sourceDir, buildDir string, onWrite func(string)) error {
	return p.hostRunGenerators(sourceDir, buildDir, onWrite)
}

func BindPackageUpdateRequireContext(p *Package, cfgVals map[string]any, options map[string]*Option) {
	p.hostUpdateRequireContext(cfgVals, options)
}

func BindCfgVals(accessor *ConfigAccessor) map[string]any {
	return accessor.cfgVals
}

func BindOptions(accessor *ConfigAccessor) map[string]*Option {
	return accessor.options
}

func BindSetCfgVals(accessor *ConfigAccessor, vals map[string]any) {
	accessor.cfgVals = vals
}

func BindSetOptions(accessor *ConfigAccessor, options map[string]*Option) {
	accessor.options = options
}

func BindPackageName(p *Package) string {
	return p.name
}

func BindPackageRepo(p *Package) string {
	return p.repo
}
