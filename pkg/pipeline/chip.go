package pipeline

import (
	"fmt"
	"slices"
	"sort"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func collectChipProfiles(ctx *RuntimeContext) error {
	profiles := make(map[string]api.ChipProfile)
	if ctx == nil || ctx.Resolver == nil || ctx.DepGraph == nil {
		if ctx != nil {
			ctx.chipProfiles = profiles
		}
		return nil
	}
	owners := make(map[string]string)
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Pkg == nil {
			continue
		}
		profile, ok := node.Pkg.ChipProfile()
		if !ok {
			continue
		}
		if existing, exists := profiles[profile.Name]; exists {
			if existing != profile {
				return fmt.Errorf("chip profile %q is declared by both %s and %s with different values",
					profile.Name, owners[profile.Name], name)
			}
			continue
		}
		profiles[profile.Name] = profile
		owners[profile.Name] = name
	}
	ctx.chipProfiles = profiles
	return nil
}

func extendChipChoice(ctx *RuntimeContext, needed NeededSet) {
	if ctx == nil || ctx.globalOptions == nil {
		return
	}
	gOpt := ctx.globalOptions[api.ChipOptionName]
	if gOpt == nil || gOpt.Type() != api.OptionChoice {
		return
	}
	values := append([]string(nil), gOpt.Values()...)
	var profileCount int
	if needed != nil && ctx.DepGraph != nil && len(ctx.DepGraph.Packages) > 0 {
		for pkgName, node := range ctx.DepGraph.Packages {
			if !needed[pkgName] || node == nil || node.Pkg == nil {
				continue
			}
			if profile, ok := node.Pkg.ChipProfile(); ok {
				profileCount++
				if !slices.Contains(values, profile.Name) {
					values = append(values, profile.Name)
				}
			}
		}
	} else {
		profileCount = len(ctx.chipProfiles)
		for chip := range ctx.chipProfiles {
			if !slices.Contains(values, chip) {
				values = append(values, chip)
			}
		}
	}
	if profileCount > 0 {
		sort.Strings(values)
	}
	if len(values) > 0 && !slices.Equal(values, gOpt.Values()) {
		clone := *gOpt
		clone.SetValues(values...)
		ctx.globalOptions[api.ChipOptionName] = &clone
	}
}

func configuredChip(ctx *RuntimeContext) string {
	if ctx == nil || ctx.Config == nil {
		return ""
	}
	value, _ := config.BuildGlobalValues(ctx.Config)[api.ChipOptionName].(string)
	return value
}

func displayChip(ctx *RuntimeContext) string {
	if value := configuredChip(ctx); value != "" {
		return value
	}
	if ctx != nil && ctx.globalOptions != nil {
		if opt := ctx.globalOptions[api.ChipOptionName]; opt != nil {
			if value, ok := opt.Default().(string); ok {
				return value
			}
		}
	}
	return ""
}

func selectedChipProfile(ctx *RuntimeContext) (api.ChipProfile, bool) {
	chip := displayChip(ctx)
	if chip == "" {
		return api.ChipProfile{}, false
	}
	profile, ok := ctx.chipProfiles[chip]
	return profile, ok
}

func explicitToolchain(ctx *RuntimeContext) bool {
	if ctx.ToolchainOverride != "" {
		return true
	}
	return ctx.Config != nil && ctx.Config.Global != nil && ctx.Config.Global.Toolchain != ""
}

func neededChipProfile(ctx *RuntimeContext, needed NeededSet) (api.ChipProfile, string, error) {
	var profile api.ChipProfile
	var owner string
	for _, name := range ctx.Resolver.GetOrder() {
		if needed != nil && !needed.Has(name) {
			continue
		}
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Pkg == nil {
			continue
		}
		candidate, ok := node.Pkg.ChipProfile()
		if !ok {
			continue
		}
		if profile.Name == "" {
			profile, owner = candidate, name
			continue
		}
		if profile != candidate {
			return api.ChipProfile{}, "", fmt.Errorf("active chip profile conflict: %s declares %q, %s declares %q",
				owner, profile.Name, name, candidate.Name)
		}
	}
	return profile, owner, nil
}

func resolveActiveChip(ctx *RuntimeContext, needed NeededSet) error {
	ctx.chipProfile = api.ChipProfile{}
	ctx.PreferredToolchain = ""
	if ctx == nil || ctx.Resolver == nil || ctx.DepGraph == nil || len(ctx.chipProfiles) == 0 {
		return nil
	}
	explicit := explicitToolchain(ctx)
	profile, owner, err := neededChipProfile(ctx, needed)
	if err != nil {
		if explicit {
			return nil
		}
		return err
	}
	configured := configuredChip(ctx)
	if profile.Name == "" {
		if configured == "" {
			return nil
		}
		if _, known := ctx.chipProfiles[configured]; known {
			return fmt.Errorf("chip option selects %q, but the active build does not use that chip", configured)
		}
		return fmt.Errorf("chip option selects %q, but no active package declares that chip profile", configured)
	}
	if configured != "" && configured != profile.Name {
		if explicit {
			ctx.chipProfile = profile
			ctx.PreferredToolchain = profile.PreferredToolchain
			return nil
		}
		return fmt.Errorf("chip option selects %q, but the active build uses chip profile %q from %s",
			configured, profile.Name, owner)
	}
	ctx.chipProfile = profile
	ctx.PreferredToolchain = profile.PreferredToolchain
	if ctx.PreferredToolchain == "" || explicit {
		return nil
	}
	if _, err := toolchain.GetManager().GetToolchain(ctx.PreferredToolchain); err != nil {
		return fmt.Errorf("chip package %s prefers toolchain %q, which is not available: %w; install or trust the toolchain extension, or select a toolchain explicitly",
			owner, ctx.PreferredToolchain, err)
	}
	return nil
}

// displayChipProfile derives the chip profile shown for the active build: the
// configured profile when it is known, otherwise the single profile declared by
// the reachable packages. It does not mutate the context.
func displayChipProfile(ctx *RuntimeContext) (api.ChipProfile, string) {
	if ctx == nil || ctx.Resolver == nil || ctx.DepGraph == nil {
		return api.ChipProfile{}, ""
	}
	if profile, ok := selectedChipProfile(ctx); ok {
		return profile, profile.PreferredToolchain
	}
	unique := make(map[api.ChipProfile]bool)
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Pkg == nil {
			continue
		}
		if profile, ok := node.Pkg.ChipProfile(); ok {
			unique[profile] = true
		}
	}
	if len(unique) != 1 {
		return api.ChipProfile{}, ""
	}
	for profile := range unique {
		return profile, profile.PreferredToolchain
	}
	return api.ChipProfile{}, ""
}

func ResolveActiveChipForDisplay(ctx *RuntimeContext) {
	profile, tc := displayChipProfile(ctx)
	if ctx == nil {
		return
	}
	ctx.chipProfile = profile
	ctx.PreferredToolchain = tc
}

func chipTargetsApply(profile api.ChipProfile, tcName string) bool {
	return api.BindChipTargetsApply(profile, tcName, func(name string) string {
		_, triple := toolchainTargetDefaults(name)
		return triple
	})
}
