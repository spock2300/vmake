package pipeline

import (
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
)

func NewConfigWorkbench(ctx *RuntimeContext) *config.Workbench {
	if ctx == nil || ctx.Resolver == nil || ctx.DepGraph == nil || ctx.Config == nil {
		return nil
	}
	options := ctx.AllOptions()
	values := make(map[string]map[string]any, len(options))
	for pkgName := range options {
		values[pkgName] = config.GetEntry(ctx.Config, pkgName).Options
	}
	var packages []buildscript.Source
	deps := make(map[string][]string)
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil {
			continue
		}
		deps[name] = node.Deps
		if node.IsLocal() && node.Source != nil {
			packages = append(packages, buildscript.Source{
				Name:   node.Source.Name,
				Path:   node.Source.Path,
				Dir:    node.Source.Dir,
				Origin: node.Source.Origin,
				Script: node.Source.Script,
			})
		}
	}
	workbench := &config.Workbench{
		Packages:         packages,
		Deps:             deps,
		Options:          options,
		Values:           values,
		DerivedOptions:   ctx.DerivedOptions(),
		BaseConfig:       ctx.Config.Clone(),
		CurrentToolchain: ResolveToolchainName(ctx.Config, "", ctx.PreferredToolchain),
		GlobalOptions:    ctx.GlobalOptions(),
		GlobalValues:     config.BuildGlobalValues(ctx.Config),
		ChipProfiles:     ctx.ChipProfiles(),
		KConfigs:         ctx.KConfigs(),
		Description:      ctx.Config.Description,
		DisplayDefaults:  ToolchainTargetDefaults,
	}
	if previewer := NewConfigPreviewer(ctx); previewer != nil {
		workbench.Preview = previewer
		if preview, ok := previewer(ctx.Config); ok {
			workbench.FocusActive = true
			workbench.InitialPreview = preview
		}
	}
	return workbench
}
