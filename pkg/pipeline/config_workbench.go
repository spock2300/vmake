package pipeline

import (
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
)

func NewConfigWorkbench(ctx *RuntimeContext) *config.Workbench {
	if ctx == nil || ctx.Resolver == nil || ctx.DepGraph == nil || ctx.Config == nil {
		return nil
	}
	rawOptions := ctx.AllOptions()
	options := make(map[string]map[string]*api.Option, len(rawOptions))
	values := make(map[string]map[string]any, len(rawOptions))
	for pkgName, pkgOpts := range rawOptions {
		if checkPackageVisibility(ctx, "@local", pkgName) != nil {
			continue
		}
		visible := make(map[string]*api.Option, len(pkgOpts))
		for optName, opt := range pkgOpts {
			if isExternalInternalOption(ctx, pkgName, opt) {
				continue
			}
			visible[optName] = opt
		}
		if len(visible) == 0 {
			continue
		}
		options[pkgName] = visible
		rawVals := config.GetEntry(ctx.Config, pkgName).Options
		if len(rawVals) > 0 {
			filtered := make(map[string]any, len(rawVals))
			for k, v := range rawVals {
				if _, ok := visible[k]; ok {
					filtered[k] = v
				}
			}
			values[pkgName] = filtered
		} else {
			values[pkgName] = map[string]any{}
		}
	}
	var packages []buildscript.Source
	deps := make(map[string][]string)
	pkgContainer := make(map[string]string, len(ctx.DepGraph.Packages))
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil {
			continue
		}
		if checkPackageVisibility(ctx, "@local", name) != nil {
			continue
		}
		var visibleDeps []string
		for _, dep := range node.Deps {
			if checkPackageVisibility(ctx, "@local", dep) == nil {
				visibleDeps = append(visibleDeps, dep)
			}
		}
		deps[name] = visibleDeps
		if node.Container != "" {
			pkgContainer[name] = node.Container
		}
		if node.IsLocal() && node.Source != nil {
			packages = append(packages, buildscript.Source{
				Name:      node.Source.Name,
				Container: node.Container,
				RelPath:   node.RelPath,
				Path:      node.Source.Path,
				Dir:       node.Source.Dir,
				Origin:    node.Source.Origin,
				Script:    node.Source.Script,
			})
		}
	}
	rawKConfigs := ctx.KConfigs()
	kconfigs := make(map[string][]*api.KConfigEntry, len(rawKConfigs))
	for pkgName, list := range rawKConfigs {
		if checkPackageVisibility(ctx, "@local", pkgName) != nil {
			continue
		}
		kconfigs[pkgName] = list
	}
	rawDerived := ctx.DerivedOptions()
	derived := make(map[string]map[string]any, len(rawDerived))
	for pkgName, vals := range rawDerived {
		if checkPackageVisibility(ctx, "@local", pkgName) != nil {
			continue
		}
		pkgOpts := rawOptions[pkgName]
		filtered := make(map[string]any, len(vals))
		for optName, val := range vals {
			if opt := pkgOpts[optName]; opt != nil && isExternalInternalOption(ctx, pkgName, opt) {
				continue
			}
			filtered[optName] = val
		}
		if len(filtered) > 0 {
			derived[pkgName] = filtered
		}
	}
	containers := make(map[string]config.WorkbenchContainer, len(ctx.DepGraph.Containers))
	for id, c := range ctx.DepGraph.Containers {
		if c == nil {
			continue
		}
		linkPath := ""
		if c.Link != nil {
			linkPath = c.Link.Path
		}
		containers[id] = config.WorkbenchContainer{
			ID:       c.ID,
			Kind:     c.Kind.String(),
			Version:  c.Version,
			Commit:   c.Commit,
			LinkPath: linkPath,
		}
	}
	configFile := ""
	if ctx.ConfigPath != "" {
		configFile = configFileFor(ctx.WorkDir, ctx.ConfigPath)
	}
	workbench := &config.Workbench{
		Packages:         packages,
		Deps:             deps,
		Containers:       containers,
		PackageContainer: pkgContainer,
		Options:          options,
		Values:           values,
		DerivedOptions:   derived,
		BaseConfig:       ctx.Config.Clone(),
		CurrentToolchain: ResolveToolchainName(ctx.Config, "", ctx.PreferredToolchain),
		GlobalOptions:    ctx.GlobalOptions(),
		GlobalValues:     config.BuildGlobalValues(ctx.Config),
		ChipProfiles:     ctx.ChipProfiles(),
		KConfigs:         kconfigs,
		ConfigFile:       configFile,
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
