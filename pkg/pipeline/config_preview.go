package pipeline

import (
	"slices"

	"github.com/spock2300/vmake/internal/quiet"
	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/internal/scriptfs"
	"github.com/spock2300/vmake/pkg/config"
)

func EnterQuietReadOnlyScope(quietLog, quietExec bool) func() {
	return quiet.ProbeScope(quietLog, quietExec)
}

func NewConfigPreviewer(ctx *RuntimeContext) func(*config.ConfigFile) (config.Preview, bool) {
	if ctx == nil || ctx.DepGraph == nil || ctx.Resolver == nil {
		return nil
	}
	base := *ctx
	eagerDeps := make(map[string][]string, len(base.DepGraph.Packages))
	for name, node := range base.DepGraph.Packages {
		if node != nil {
			eagerDeps[name] = slices.Clone(node.Deps)
		}
	}

	restoreEager := func() {
		for name, deps := range eagerDeps {
			if node := base.DepGraph.Packages[name]; node != nil {
				node.Deps = slices.Clone(deps)
			}
		}
		_ = base.Resolver.UpdateOrder()
	}

	return func(draft *config.ConfigFile) (_ config.Preview, ok bool) {
		var recErr error
		defer func() {
			restoreEager()
			if recErr != nil {
				ok = false
			}
		}()
		defer scriptcall.Recover(&recErr)

		restoreScope := EnterQuietReadOnlyScope(true, true)
		defer func() {
			_ = scriptfs.TakeBlockedWrites()
			restoreScope()
		}()

		restoreEager()
		work := base
		if draft != nil {
			work.Config = draft.Clone()
		} else if base.Config != nil {
			work.Config = base.Config.Clone()
		} else {
			work.Config = &config.ConfigFile{}
		}

		rootOverrides := configRootOverrides(&work)
		owners, err := convergeFilterAndDerivedOptionsWithOwners(&work, rootOverrides, false)
		if err != nil {
			return config.Preview{}, false
		}
		needed, err := computeNeededGraph(work.DepGraph, rootOverrides)
		if err != nil {
			return config.Preview{}, false
		}

		activeDeps := make(map[string][]string, len(work.DepGraph.Packages))
		for name, node := range work.DepGraph.Packages {
			if node == nil || checkPackageVisibility(&work, "@local", name) != nil {
				continue
			}
			var visibleDeps []string
			for _, dep := range node.Deps {
				if checkPackageVisibility(&work, "@local", dep) == nil {
					visibleDeps = append(visibleDeps, dep)
				}
			}
			activeDeps[name] = visibleDeps
		}
		activePackages := make(map[string]bool, len(needed.Set))
		for name, ok := range needed.Set {
			if ok && checkPackageVisibility(&work, "@local", name) == nil {
				activePackages[name] = true
			}
		}
		derivedOpts := make(map[string]map[string]any, len(work.derivedOptions))
		for pkg, m := range work.derivedOptions {
			if checkPackageVisibility(&work, "@local", pkg) != nil {
				continue
			}
			pkgOpts := work.allOptions[pkg]
			filtered := make(map[string]any, len(m))
			for optName, val := range m {
				if opt := pkgOpts[optName]; opt != nil && isExternalInternalOption(&work, pkg, opt) {
					continue
				}
				filtered[optName] = val
			}
			if len(filtered) > 0 {
				derivedOpts[pkg] = filtered
			}
		}
		derivedOwners := make(map[string]map[string]string, len(owners))
		for pkg, m := range owners {
			if checkPackageVisibility(&work, "@local", pkg) != nil {
				continue
			}
			pkgOpts := work.allOptions[pkg]
			filtered := make(map[string]string, len(m))
			for optName, owner := range m {
				if opt := pkgOpts[optName]; opt != nil && isExternalInternalOption(&work, pkg, opt) {
					continue
				}
				filtered[optName] = owner
			}
			if len(filtered) > 0 {
				derivedOwners[pkg] = filtered
			}
		}

		return config.Preview{
			Roots:          slices.Clone(needed.Roots),
			ActivePackages: activePackages,
			ActiveDeps:     activeDeps,
			DerivedOptions: derivedOpts,
			DerivedOwners:  derivedOwners,
		}, true
	}
}
