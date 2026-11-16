package pipeline

import (
	"maps"
	"slices"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/internal/scriptfs"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
)

type ConfigPreview struct {
	Roots          []string
	ActivePackages map[string]bool
	ActiveDeps     map[string][]string
	DerivedOptions map[string]map[string]any
	DerivedOwners  map[string]map[string]string
}

func EnterQuietReadOnlyScope(quiet, execQuiet bool) func() {
	scriptfs.SetReadOnly(true)
	previousLevel := vlog.GetLevel()
	previousQuiet := iexec.IsQuiet()
	if quiet {
		vlog.SetLevel(vlog.Quiet)
	}
	if execQuiet {
		iexec.SetQuiet(true)
	}
	return func() {
		scriptfs.SetReadOnly(false)
		vlog.SetLevel(previousLevel)
		iexec.SetQuiet(previousQuiet)
	}
}

func NewConfigPreviewer(ctx *RuntimeContext) func(*config.ConfigFile) (ConfigPreview, bool) {
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

	return func(draft *config.ConfigFile) (_ ConfigPreview, ok bool) {
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

		rootOverride := configRootOverride(&work)
		owners, err := convergeFilterAndDerivedOptionsWithOwners(&work, rootOverride, false)
		if err != nil {
			return ConfigPreview{}, false
		}
		needed, err := computeNeededGraph(work.DepGraph, rootOverride)
		if err != nil {
			return ConfigPreview{}, false
		}

		activeDeps := make(map[string][]string, len(work.DepGraph.Packages))
		for name, node := range work.DepGraph.Packages {
			if node != nil {
				activeDeps[name] = slices.Clone(node.Deps)
			}
		}
		derivedOpts := make(map[string]map[string]any, len(work.derivedOptions))
		for pkg, m := range work.derivedOptions {
			derivedOpts[pkg] = maps.Clone(m)
		}
		derivedOwners := make(map[string]map[string]string, len(owners))
		for pkg, m := range owners {
			derivedOwners[pkg] = maps.Clone(m)
		}

		return ConfigPreview{
			Roots:          slices.Clone(needed.Roots),
			ActivePackages: needed.Set.Clone(),
			ActiveDeps:     activeDeps,
			DerivedOptions: derivedOpts,
			DerivedOwners:  derivedOwners,
		}, true
	}
}
