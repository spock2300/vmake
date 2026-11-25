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
	pkgContainer := make(map[string]string, len(ctx.DepGraph.Packages))
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil {
			continue
		}
		deps[name] = node.Deps
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
		DerivedOptions:   ctx.DerivedOptions(),
		BaseConfig:       ctx.Config.Clone(),
		CurrentToolchain: ResolveToolchainName(ctx.Config, "", ctx.PreferredToolchain),
		GlobalOptions:    ctx.GlobalOptions(),
		GlobalValues:     config.BuildGlobalValues(ctx.Config),
		ChipProfiles:     ctx.ChipProfiles(),
		KConfigs:         ctx.KConfigs(),
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
