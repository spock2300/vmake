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
			ctx.ChipProfiles = profiles
		}
		return nil
	}
	owners := make(map[string]string)
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Pkg == nil || !node.IsLocal() {
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
	ctx.ChipProfiles = profiles
	return nil
}

func extendChipChoice(ctx *RuntimeContext) {
	if ctx == nil || len(ctx.ChipProfiles) == 0 {
		return
	}
	extend := func(opts map[string]*api.Option) {
		if opts == nil {
			return
		}
		opt := opts[api.ChipOptionName]
		if opt == nil || opt.Type() != api.OptionChoice {
			return
		}
		values := append([]string(nil), opt.Values()...)
		for chip := range ctx.ChipProfiles {
			if !slices.Contains(values, chip) {
				values = append(values, chip)
			}
		}
		sort.Strings(values)
		opt.SetValues(values...)
	}
	extend(ctx.GlobalOptions)
	for _, opts := range ctx.AllOptions {
		extend(opts)
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
	if ctx != nil && ctx.GlobalOptions != nil {
		if opt := ctx.GlobalOptions[api.ChipOptionName]; opt != nil {
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
	profile, ok := ctx.ChipProfiles[chip]
	return profile, ok
}

func explicitToolchain(ctx *RuntimeContext) bool {
	if ctx.ToolchainOverride != "" {
		return true
	}
	return ctx.Config != nil && ctx.Config.Global != nil && ctx.Config.Global.Toolchain != ""
}

func neededChipProfile(ctx *RuntimeContext, needed map[string]bool) (api.ChipProfile, string, error) {
	var profile api.ChipProfile
	var owner string
	for _, name := range ctx.Resolver.GetOrder() {
		if needed != nil && !needed[name] {
			continue
		}
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Pkg == nil || !node.IsLocal() {
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

func resolveActiveChip(ctx *RuntimeContext, needed map[string]bool) error {
	ctx.ChipProfile = api.ChipProfile{}
	ctx.PreferredToolchain = ""
	if ctx == nil || ctx.Resolver == nil || ctx.DepGraph == nil || len(ctx.ChipProfiles) == 0 {
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
		if _, known := ctx.ChipProfiles[configured]; known {
			return fmt.Errorf("chip option selects %q, but the active build does not use that chip", configured)
		}
		return fmt.Errorf("chip option selects %q, but no active package declares that chip profile", configured)
	}
	if configured != "" && configured != profile.Name {
		if explicit {
			ctx.ChipProfile = profile
			ctx.PreferredToolchain = profile.PreferredToolchain
			return nil
		}
		return fmt.Errorf("chip option selects %q, but the active build uses chip profile %q from %s",
			configured, profile.Name, owner)
	}
	ctx.ChipProfile = profile
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

func ResolveActiveChipForDisplay(ctx *RuntimeContext) {
	if ctx == nil || ctx.Resolver == nil || ctx.DepGraph == nil {
		return
	}
	ctx.ChipProfile = api.ChipProfile{}
	ctx.PreferredToolchain = ""
	if profile, ok := selectedChipProfile(ctx); ok {
		ctx.ChipProfile = profile
		ctx.PreferredToolchain = profile.PreferredToolchain
		return
	}
	unique := make(map[api.ChipProfile]bool)
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Pkg == nil || !node.IsLocal() {
			continue
		}
		if profile, ok := node.Pkg.ChipProfile(); ok {
			unique[profile] = true
		}
	}
	if len(unique) != 1 {
		return
	}
	for profile := range unique {
		ctx.ChipProfile = profile
		ctx.PreferredToolchain = profile.PreferredToolchain
	}
}

func chipTargetsApply(profile api.ChipProfile, tcName string) bool {
	if profile.Name == "" {
		return false
	}
	if profile.PreferredToolchain == "" || tcName == profile.PreferredToolchain {
		return true
	}
	if tcName == "" || tcName == "host" {
		return false
	}
	_, triple := toolchainTargetDefaults(tcName)
	return triple == "" || triple == profile.TargetTriple
}
