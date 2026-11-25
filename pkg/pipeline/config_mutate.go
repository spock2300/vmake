package pipeline

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/spock2300/vmake/internal/pkgvis"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
)

type ConfigMutation struct {
	Sets   []SetItem
	Unsets []UnsetItem
}

type SetItem struct {
	Pkg string
	Opt string
	Raw string
}

type UnsetItem struct {
	Pkg string
	Opt string
}

type AppliedChange struct {
	Kind  string
	Pkg   string
	Opt   string
	Raw   string
	Value any
}

func (c AppliedChange) Key() string {
	if c.Pkg == "" {
		return c.Opt
	}
	return c.Pkg + ":" + c.Opt
}

func ResolveSetPackageOption(ctx *RuntimeContext, pkgName, optName string) (string, *api.Option, error) {
	if pkgName == "" {
		var globals map[string]*api.Option
		if ctx != nil {
			globals = ctx.globalOptions
		}
		opt, ok := globals[optName]
		if !ok {
			available := sortedOptionNames(globals)
			return "", nil, fmt.Errorf("global option %q not found (available: %s)", optName, strings.Join(available, ", "))
		}
		return "", opt, nil
	}

	resolvedPkg := pkgName
	if ctx != nil && ctx.Resolver != nil {
		if err := pkgvis.CheckInternal("@local", "@local", resolvedPkg, ctx.Resolver.VisibilityPath(resolvedPkg)); err != nil {
			return "", nil, err
		}
	}
	var pkgOpts map[string]*api.Option
	var hasOpts bool
	if ctx != nil {
		pkgOpts, hasOpts = ctx.allOptions[resolvedPkg]
	}
	inGraph := ctx != nil && ctx.DepGraph != nil && ctx.DepGraph.Packages[resolvedPkg] != nil
	if !hasOpts && inGraph {
		if node := ctx.DepGraph.Packages[resolvedPkg]; node != nil && node.Pkg != nil {
			pkgOpts = node.Pkg.GetOptions()
			hasOpts = len(pkgOpts) > 0
		}
	}
	if !hasOpts && !inGraph {
		var allOpts map[string]map[string]*api.Option
		if ctx != nil {
			allOpts = ctx.allOptions
		}
		available := sortedPackageNames(allOpts)
		if len(available) == 0 {
			return "", nil, fmt.Errorf("package %q not found", pkgName)
		}
		return "", nil, fmt.Errorf("package %q not found (available: %s)", pkgName, strings.Join(available, ", "))
	}

	opt, ok := pkgOpts[optName]
	if !ok {
		available := sortedOptionNames(pkgOpts)
		return "", nil, fmt.Errorf("option %q not found in package %q (available: %s)", optName, resolvedPkg, strings.Join(available, ", "))
	}
	if opt.IsGlobal() {
		return "", nil, fmt.Errorf("option %q in package %q is global; set it without a package prefix: %s=...", optName, resolvedPkg, optName)
	}
	return resolvedPkg, opt, nil
}

func hasExplicitConfigKey(ctx *RuntimeContext, pkgName, optName string) (string, bool) {
	if ctx == nil || ctx.Config == nil {
		return pkgName, false
	}
	if pkgName == "" {
		return "", ctx.Config.HasExplicitOption("", optName)
	}
	if ctx.Config.HasExplicitOption(pkgName, optName) {
		return pkgName, true
	}
	return pkgName, false
}

func MutateConfig(ctx *RuntimeContext, req ConfigMutation) ([]AppliedChange, error) {
	if ctx == nil || ctx.Config == nil {
		return nil, fmt.Errorf("project configuration is not initialized")
	}
	changes := make([]AppliedChange, 0, len(req.Unsets)+len(req.Sets))

	for _, u := range req.Unsets {
		resolvedPkg, _, err := ResolveSetPackageOption(ctx, u.Pkg, u.Opt)
		if err != nil {
			if existingPkg, ok := hasExplicitConfigKey(ctx, u.Pkg, u.Opt); ok {
				resolvedPkg = existingPkg
			} else {
				return nil, err
			}
		}
		ctx.Config.UnsetOption(resolvedPkg, u.Opt)
		changes = append(changes, AppliedChange{
			Kind: "unset",
			Pkg:  resolvedPkg,
			Opt:  u.Opt,
		})
	}

	for _, s := range req.Sets {
		resolvedPkg, opt, err := ResolveSetPackageOption(ctx, s.Pkg, s.Opt)
		if err != nil {
			return nil, err
		}
		prefix := s.Opt
		if resolvedPkg != "" {
			prefix = resolvedPkg + ":" + s.Opt
		}
		value, err := config.CoerceOptionValue(opt, s.Raw, prefix)
		if err != nil {
			return nil, err
		}
		ctx.Config.SetOption(resolvedPkg, s.Opt, value)
		changes = append(changes, AppliedChange{
			Kind:  "set",
			Pkg:   resolvedPkg,
			Opt:   s.Opt,
			Raw:   s.Raw,
			Value: value,
		})
	}

	return changes, nil
}

func sortedOptionNames(opts map[string]*api.Option) []string {
	return slices.Sorted(maps.Keys(opts))
}

func sortedPackageNames(pkgs map[string]map[string]*api.Option) []string {
	return slices.Sorted(maps.Keys(pkgs))
}
