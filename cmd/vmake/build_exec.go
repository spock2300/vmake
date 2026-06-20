package main

import (
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/resolver"
)

func mergeCfgVals(name string, node *resolver.PackageNode, ctx *RuntimeContext, globalValues map[string]any, allPkgOptions map[string]map[string]any) map[string]any {
	cfgVals := make(map[string]any)
	allOpts := ctx.AllOptions[name]
	if allOpts == nil && node.Pkg != nil {
		allOpts = node.Pkg.GetOptions()
	}
	if allOpts != nil {
		for optName, opt := range allOpts {
			if opt.Default() != nil {
				cfgVals[optName] = opt.Default()
			}
		}
	}
	if opts, ok := allPkgOptions[name]; ok {
		for k, v := range opts {
			cfgVals[k] = v
		}
	}
	for k, v := range globalValues {
		if _, exists := cfgVals[k]; !exists {
			cfgVals[k] = v
		}
	}
	return cfgVals
}

func enableTestDefaults(allTargets map[string]map[string]*api.Target) {
	for _, targets := range allTargets {
		for _, t := range targets {
			if t.IsTest() {
				t.SetDefault(true)
			}
		}
	}
}

func applyBuildContextConfig(buildCtx *api.BuildContext, node *resolver.PackageNode, ctx *RuntimeContext, currentPkg string) {
	subParents := ctx.Resolver.SubParents()
	if buildCtx.GenConfigDefines() && node.Pkg != nil {
		var importPkgs []*api.Package
		for _, depName := range buildCtx.ImportConfigs() {
			resolved := api.ResolveSubPackageName(currentPkg, depName, subParents, func(candidate string) bool {
				_, ok := ctx.DepGraph.Packages[candidate]
				return ok
			})
			depNode := ctx.DepGraph.Packages[resolved]
			if depNode != nil && depNode.Pkg != nil {
				importPkgs = append(importPkgs, depNode.Pkg)
			}
		}
		mergedOpts, mergedVals := api.MergeImportedOptions(node.Pkg.Options, node.Pkg.CfgVals, importPkgs)
		defines := api.ConfigToDefines(mergedOpts, mergedVals)
		args := make([]any, len(defines))
		for i, d := range defines {
			args[i] = d
		}
		for _, t := range buildCtx.GetTargets() {
			t.AddDefines(args...)
		}
	}
	if buildCtx.ExportEnabled() && node.Pkg != nil {
		node.Pkg.SetExportConfig(true)
	}
	if imports := buildCtx.ImportConfigs(); len(imports) > 0 && node.Pkg != nil {
		node.Pkg.SetImportConfigs(imports)
	}
	if buildCtx.GenConfigHeader() && node.Pkg != nil {
		node.Pkg.SetGenConfigHeader(true)
	}
}
