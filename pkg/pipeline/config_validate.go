package pipeline

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func ValidateConfigValues(ctx *RuntimeContext, needed map[string]bool) error {
	return validateConfigValuesInternal(ctx, needed, false)
}

func validateConfigValuesInternal(ctx *RuntimeContext, needed map[string]bool, checkToolchain bool) error {
	if ctx == nil || ctx.Config == nil {
		return nil
	}
	if rootOverride := configRootOverride(ctx); rootOverride != "" && ctx.DepGraph != nil {
		if _, err := computeRootSeeds(ctx.DepGraph, rootOverride); err != nil {
			return err
		}
	}
	if ctx.Config.Global != nil {
		if ctx.Config.Global.Mode != "" {
			if modeOpt := ctx.GlobalOptions[api.ModeOptionName]; modeOpt != nil {
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
			if opt := ctx.GlobalOptions[k]; opt != nil {
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
		if needed != nil && !needed[pkgName] {
			continue
		}
		entry := ctx.Config.Entries[pkgName]
		if entry == nil {
			continue
		}
		pkgOpts := ctx.AllOptions[pkgName]
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
	needed, err := resolveNeeded(ctx, false, false, true)
	if err != nil {
		return nil, err
	}
	if err := mergeGlobalOptionsFiltered(ctx, needed); err != nil {
		return nil, err
	}
	extendChipChoice(ctx, needed)

	var issues []string
	if valErr := validateConfigValuesInternal(ctx, needed, true); valErr != nil {
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
			if ctx.GlobalOptions[k] == nil {
				avail := sortedOptMapKeys(ctx.GlobalOptions)
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
			issues = append(issues, fmt.Sprintf("entries.%s: package not found in workspace", pkgName))
			continue
		}
		if !needed[pkgName] {
			issues = append(issues, fmt.Sprintf("entries.%s: package is not reachable from active root", pkgName))
			continue
		}
		pkgOpts := ctx.AllOptions[pkgName]
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
				if gOpt := ctx.GlobalOptions[k]; gOpt != nil {
					issues = append(issues, fmt.Sprintf("entries.%s.options.%s: option is global; set it under global instead", pkgName, k))
					continue
				}
				avail := sortedNonGlobalOptMapKeys(pkgOpts)
				if len(avail) == 0 {
					issues = append(issues, fmt.Sprintf("entries.%s.options.%s: unknown option (package declares no options)", pkgName, k))
				} else {
					issues = append(issues, fmt.Sprintf("entries.%s.options.%s: unknown option (available: %s)", pkgName, k, strings.Join(avail, ", ")))
				}
				continue
			}
			if opt.IsGlobal() {
				issues = append(issues, fmt.Errorf("entries.%s.options.%s: option is global; set it under global instead", pkgName, k).Error())
			}
		}
		if (entry.KConfig != "" || entry.SelectedPreset != "") && len(ctx.AllKConfigs[pkgName]) == 0 {
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
				pkgDirs := ResolveAllPackageDirs(ctx.DepGraph)
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

func sortedNonGlobalOptMapKeys(opts map[string]*api.Option) []string {
	keys := make([]string, 0, len(opts))
	for k, opt := range opts {
		if opt != nil && !opt.IsGlobal() {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}
