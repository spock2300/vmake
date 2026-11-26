package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func ValidateConfigValues(ctx *RuntimeContext, needed NeededSet) error {
	return validateConfigValuesInternal(ctx, needed, false, true)
}

func validateConfigVisibility(ctx *RuntimeContext) error {
	if ctx == nil || ctx.Config == nil || len(ctx.Config.Entries) == 0 {
		return nil
	}
	pkgNames := make([]string, 0, len(ctx.Config.Entries))
	for pkgName := range ctx.Config.Entries {
		pkgNames = append(pkgNames, pkgName)
	}
	sort.Strings(pkgNames)
	for _, pkgName := range pkgNames {
		entry := ctx.Config.Entries[pkgName]
		if entry == nil {
			continue
		}
		if err := checkPackageVisibility(ctx, "@local", pkgName); err != nil {
			return fmt.Errorf("entries.%s: %w", pkgName, err)
		}
		pkgOpts := ctx.allOptions[pkgName]
		if len(entry.Options) == 0 || len(pkgOpts) == 0 {
			continue
		}
		optNames := make([]string, 0, len(entry.Options))
		for k := range entry.Options {
			optNames = append(optNames, k)
		}
		sort.Strings(optNames)
		for _, k := range optNames {
			if opt := pkgOpts[k]; opt != nil && !opt.IsGlobal() && isExternalInternalOption(ctx, pkgName, opt) {
				return fmt.Errorf("entries.%s.options.%s: option is internal to container %q and cannot be set externally", pkgName, k, packageContainerID(ctx, pkgName))
			}
		}
	}
	return nil
}

func validateConfigValuesInternal(ctx *RuntimeContext, needed NeededSet, checkToolchain, checkVisibility bool) error {
	if ctx == nil || ctx.Config == nil {
		return nil
	}
	if checkVisibility {
		if err := validateConfigVisibility(ctx); err != nil {
			return err
		}
	}
	if rootOverrides := configRootOverrides(ctx); len(rootOverrides) > 0 && ctx.DepGraph != nil {
		if _, err := computeRootSeeds(ctx.DepGraph, rootOverrides); err != nil {
			return err
		}
	}
	if ctx.Config.Global != nil {
		if ctx.Config.Global.Mode != "" {
			if modeOpt := ctx.globalOptions[api.ModeOptionName]; modeOpt != nil {
				if err := api.ValidateOptionValue(modeOpt, ctx.Config.Global.Mode); err != nil {
					return fmt.Errorf("global.mode: %w", err)
				}
			}
		}
		if checkToolchain && ctx.Config.Global.Toolchain != "" {
			if _, err := toolchain.GetManager().GetToolchain(ctx.Config.Global.Toolchain); err != nil {
				return fmt.Errorf("global.toolchain: %w", err)
			}
		}
		optNames := make([]string, 0, len(ctx.Config.Global.Options))
		for k := range ctx.Config.Global.Options {
			optNames = append(optNames, k)
		}
		sort.Strings(optNames)
		for _, k := range optNames {
			val := ctx.Config.Global.Options[k]
			if k == api.TargetOSOptionName || k == api.TargetTripleOptionName {
				if val != nil {
					if _, ok := val.(string); !ok {
						return fmt.Errorf("project option %s must be a string", k)
					}
				}
				continue
			}
			if opt := ctx.globalOptions[k]; opt != nil {
				norm := api.NormalizeOptionValue(opt, val)
				if err := api.ValidateOptionValue(opt, norm); err != nil {
					return fmt.Errorf("global.options.%s: %w", k, err)
				}
			}
		}
	}

	pkgNames := make([]string, 0, len(ctx.Config.Entries))
	for pkgName := range ctx.Config.Entries {
		pkgNames = append(pkgNames, pkgName)
	}
	sort.Strings(pkgNames)
	for _, pkgName := range pkgNames {
		if needed != nil && !needed.Has(pkgName) {
			continue
		}
		entry := ctx.Config.Entries[pkgName]
		if entry == nil {
			continue
		}
		pkgOpts := ctx.allOptions[pkgName]
		optNames := make([]string, 0, len(entry.Options))
		for k := range entry.Options {
			optNames = append(optNames, k)
		}
		sort.Strings(optNames)
		for _, k := range optNames {
			val := entry.Options[k]
			switch k {
			case api.ToolchainOptionName:
				s, ok := val.(string)
				if !ok {
					return fmt.Errorf("entries.%s.options.toolchain: expected string, got %T", pkgName, val)
				}
				if checkToolchain && s != "" {
					if _, err := toolchain.GetManager().GetToolchain(s); err != nil {
						return fmt.Errorf("entries.%s.options.toolchain: %w", pkgName, err)
					}
				}
				continue
			case api.TargetOSOptionName, api.TargetTripleOptionName:
				if val != nil {
					if _, ok := val.(string); !ok {
						return fmt.Errorf("package %s option %s must be a string", pkgName, k)
					}
				}
				continue
			}
			if opt := pkgOpts[k]; opt != nil && !opt.IsGlobal() {
				norm := api.NormalizeOptionValue(opt, val)
				if err := api.ValidateOptionValue(opt, norm); err != nil {
					return fmt.Errorf("entries.%s.options.%s: %w", pkgName, k, err)
				}
			}
		}
	}
	return nil
}

func ValidateConfigStrict(ctx *RuntimeContext) ([]string, error) {
	if ctx == nil || ctx.Config == nil || ctx.DepGraph == nil {
		return nil, fmt.Errorf("project context is not initialized")
	}
	needed, err := resolveNeeded(ctx, false, false)
	if err != nil {
		return nil, err
	}
	if err := mergeGlobalOptionsFiltered(ctx, needed); err != nil {
		return nil, err
	}
	extendChipChoice(ctx, needed)

	var issues []string
	if valErr := validateConfigValuesInternal(ctx, needed, true, false); valErr != nil {
		issues = append(issues, valErr.Error())
	}

	if ctx.Config.Global != nil {
		optNames := make([]string, 0, len(ctx.Config.Global.Options))
		for k := range ctx.Config.Global.Options {
			optNames = append(optNames, k)
		}
		sort.Strings(optNames)
		for _, k := range optNames {
			if k == api.TargetOSOptionName || k == api.TargetTripleOptionName {
				continue
			}
			if k == api.ModeOptionName || k == api.ToolchainOptionName {
				issues = append(issues, fmt.Sprintf("global.options.%s: set global.%s at the global object level rather than inside global.options", k, k))
				continue
			}
			if ctx.globalOptions[k] == nil {
				avail := sortedOptMapKeys(ctx.globalOptions)
				issues = append(issues, fmt.Sprintf("global.options.%s: unknown global option (available: %s)", k, strings.Join(avail, ", ")))
			}
		}
	}

	pkgNames := make([]string, 0, len(ctx.Config.Entries))
	for pkgName := range ctx.Config.Entries {
		pkgNames = append(pkgNames, pkgName)
	}
	sort.Strings(pkgNames)
	for _, pkgName := range pkgNames {
		entry := ctx.Config.Entries[pkgName]
		if entry == nil {
			continue
		}
		if ctx.DepGraph.Packages[pkgName] == nil {
			if rel, ok := api.LocalRelPath(pkgName); ok && ctx.WorkDir != "" {
				dir := ctx.WorkDir
				if rel != "" {
					dir = filepath.Join(ctx.WorkDir, filepath.FromSlash(rel))
				}
				if _, err := os.Stat(filepath.Join(dir, "build.go")); err == nil {
					issues = append(issues, fmt.Sprintf("entries.%s: package is not reachable from active root", pkgName))
					continue
				}
			}
			issues = append(issues, fmt.Sprintf("entries.%s: package not found in workspace", pkgName))
			continue
		}
		if !needed[pkgName] {
			issues = append(issues, fmt.Sprintf("entries.%s: package is not reachable from active root", pkgName))
			continue
		}
		if err := checkPackageVisibility(ctx, "@local", pkgName); err != nil {
			issues = append(issues, fmt.Sprintf("entries.%s: %v", pkgName, err))
			continue
		}
		pkgOpts := ctx.allOptions[pkgName]
		optNames := make([]string, 0, len(entry.Options))
		for k := range entry.Options {
			optNames = append(optNames, k)
		}
		sort.Strings(optNames)
		for _, k := range optNames {
			if k == api.ToolchainOptionName || k == api.TargetOSOptionName || k == api.TargetTripleOptionName {
				continue
			}
			opt := pkgOpts[k]
			if opt == nil {
				if gOpt := ctx.globalOptions[k]; gOpt != nil {
					issues = append(issues, fmt.Sprintf("entries.%s.options.%s: option is global; set it under global instead", pkgName, k))
					continue
				}
				avail := sortedVisibleOptionNames(ctx, pkgName, pkgOpts)
				if len(avail) == 0 {
					issues = append(issues, fmt.Sprintf("entries.%s.options.%s: unknown option (package declares no options)", pkgName, k))
				} else {
					issues = append(issues, fmt.Sprintf("entries.%s.options.%s: unknown option (available: %s)", pkgName, k, strings.Join(avail, ", ")))
				}
				continue
			}
			if opt.IsGlobal() {
				issues = append(issues, fmt.Errorf("entries.%s.options.%s: option is global; set it under global instead", pkgName, k).Error())
			} else if isExternalInternalOption(ctx, pkgName, opt) {
				issues = append(issues, fmt.Sprintf("entries.%s.options.%s: option is internal to container %q and cannot be set externally", pkgName, k, packageContainerID(ctx, pkgName)))
			}
		}
		if (entry.KConfig != "" || entry.SelectedPreset != "") && len(ctx.allKConfigs[pkgName]) == 0 {
			issues = append(issues, fmt.Sprintf("entries.%s: kconfig/selected_preset configured, but package declares no KConfig", pkgName))
		}
	}

	if len(issues) == 0 {
		if err := resolveDerivedOptions(ctx, needed); err != nil {
			issues = append(issues, err.Error())
		} else if err := resolveActiveChip(ctx, needed); err != nil {
			issues = append(issues, err.Error())
		} else {
			applyConfigCallbacksForNeeded(ctx, needed, false)
			if err := updateGlobalMacroDefines(ctx); err != nil {
				issues = append(issues, err.Error())
			} else if cfg, cfgErr := resolveBuildConfig(ctx, true); cfgErr != nil {
				issues = append(issues, cfgErr.Error())
			} else {
				pkgDirs := packageSourceDirs(ctx)
				for _, name := range ctx.Resolver.GetOrder() {
					if !needed[name] {
						continue
					}
					node := ctx.DepGraph.Packages[name]
					if node == nil || node.Pkg == nil || !IsPackageSourcePresent(ctx, name, node) {
						continue
					}
					dirs := pkgDirs[name]
					if dirs == nil {
						dirs = &api.PkgDirs{SourceDir: ExpectedPackageSourceDir(ctx, name, node)}
					}
					if _, declErr := DeclareTargets(ctx, name, dirs, cfg.Tc, cfg.GlobalValues); declErr != nil {
						issues = append(issues, fmt.Sprintf("package %s: %v", name, declErr))
					}
				}
			}
		}
	}

	return issues, nil
}

func sortedOptMapKeys(opts map[string]*api.Option) []string {
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
