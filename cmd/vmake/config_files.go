package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/scriptfs"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/resolver"
)

func newConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List project configuration files and the active selection",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			configs, err := config.ListProjectConfigs(findProjectDir())
			if err != nil {
				return err
			}
			for _, cfg := range configs {
				marker := " "
				if cfg.Active {
					marker = "*"
				}
				status := ""
				if cfg.Unsaved {
					status = " (not saved yet)"
				} else if cfg.Error != nil {
					status = fmt.Sprintf(" (invalid: %v)", cfg.Error)
				}
				description := ""
				if desc := displayDescription(cfg.Description); desc != "" {
					description = "  " + desc
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s%s%s\n", marker, cfg.Name, status, description)
			}
			return nil
		},
	}
}

func newConfigDescribeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "describe [text]",
		Short: "Print or set the description of a configuration",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := findProjectDir()
			if len(args) == 1 {
				commandStorageLocks()
				path, err := config.SetProjectDescriptionWithOverride(root, configFlag, args[0])
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Description saved to %s\n", path)
				return nil
			}
			cfg, _, err := config.LoadProjectWithOverride(root, configFlag)
			if err != nil {
				return err
			}
			if cfg.Description != "" {
				fmt.Fprintln(cmd.OutOrStdout(), cfg.Description)
			}
			return nil
		},
	}
	addConfigFlag(cmd)
	return cmd
}

func newConfigUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "use <profile>",
		Short:             "Select an existing configuration in .vmake/local.json",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeConfigFilename,
		RunE: func(cmd *cobra.Command, args []string) error {
			commandStorageLocks()
			root := findProjectDir()
			sel, err := config.UseProjectConfig(root, args[0])
			if err != nil {
				return err
			}
			if err := ensureLocalLinkIgnored(root); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Active configuration: %s\n", filepath.Join(root, filepath.FromSlash(sel)))
			return nil
		},
	}
}

func newConfigCopyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "copy <dest>",
		Short: "Copy a configuration to a new file under configs/ without switching",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			commandStorageLocks()
			root := findProjectDir()
			dstPath, err := config.CopyProjectConfigWithOverride(root, configFlag, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Configuration copied to %s\n", dstPath)
			return nil
		},
	}
	addConfigFlag(cmd)
	return cmd
}

func newConfigCheckCmd() *cobra.Command {
	var checkAll bool
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Validate configuration files against declared package options and build rules",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if checkAll && strings.TrimSpace(configFlag) != "" {
				return &usageError{err: fmt.Errorf("--all cannot be combined with -c/--config")}
			}
			root := findProjectDir()
			type targetProfile struct {
				label    string
				override string
				loadErr  error
			}
			var targets []targetProfile
			if checkAll {
				configs, err := config.ListProjectConfigs(root)
				if err != nil {
					return err
				}
				for _, cfg := range configs {
					if cfg.Unsaved {
						continue
					}
					targets = append(targets, targetProfile{
						label:    cfg.Name,
						override: cfg.Name,
						loadErr:  cfg.Error,
					})
				}
				if len(targets) == 0 {
					return fmt.Errorf("no saved configuration files found under %s/", config.ConfigsDirName)
				}
			} else {
				override := strings.TrimSpace(configFlag)
				label := override
				if override != "" {
					if sel, _, err := config.ResolveConfigReference(root, override); err == nil {
						label = sel
					}
				} else {
					_, path, err := config.LoadProject(root)
					if err != nil {
						return err
					}
					if rel, relErr := filepath.Rel(root, path); relErr == nil {
						label = filepath.ToSlash(rel)
					} else {
						label = path
					}
				}
				targets = append(targets, targetProfile{label: label, override: override})
			}

			out := cmd.OutOrStdout()
			scriptfs.SetReadOnly(true)
			defer scriptfs.SetReadOnly(false)
			vlog.SetLevel(vlog.Quiet)
			iexec.SetQuiet(true)

			failed := 0
			for _, target := range targets {
				if target.loadErr != nil {
					failed++
					fmt.Fprintf(out, "FAIL %s: %v\n", target.label, target.loadErr)
					continue
				}
				issues, err := checkSingleConfigProfile(cmd, target.override)
				if err != nil {
					failed++
					fmt.Fprintf(out, "FAIL %s: %v\n", target.label, err)
					continue
				}
				if len(issues) > 0 {
					failed++
					for _, issue := range issues {
						fmt.Fprintf(out, "FAIL %s: %s\n", target.label, issue)
					}
					continue
				}
				fmt.Fprintf(out, "OK   %s\n", target.label)
			}
			if failed > 0 {
				return fmt.Errorf("configuration check failed (%d/%d invalid)", failed, len(targets))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&checkAll, "all", false, "check all configuration files under configs/")
	addConfigFlag(cmd)
	return cmd
}

func checkSingleConfigProfile(cmd *cobra.Command, configOverride string) ([]string, error) {
	restoreStdout := silenceStdout()
	defer restoreStdout()
	restoreStderr := silenceStderr()
	defer restoreStderr()

	ctx, err := openProject(projectOptions{
		execution:      cmd.Context(),
		policy:         &resolver.LockPolicy{DryRun: true},
		configOverride: configOverride,
		skipGitignore:  true,
	})
	if err != nil {
		return nil, err
	}
	defer closeProject(ctx)

	issues, err := pipeline.ValidateConfigStrict(ctx)
	scriptfs.TakeBlockedWrites()
	if err != nil {
		return nil, err
	}
	return issues, nil
}

func completeConfigFilename(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	root := findProjectDirSoft()
	if root == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	configs, err := config.ListProjectConfigs(root)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	var names []string
	for _, cfg := range configs {
		if cfg.Error == nil && !cfg.Unsaved {
			name := cfg.Name
			if desc := displayDescription(cfg.Description); desc != "" {
				name += "\t" + desc
			}
			names = append(names, name)
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func displayDescription(desc string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, desc)
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if utf8.RuneCountInString(cleaned) <= 80 {
		return cleaned
	}
	return string([]rune(cleaned)[:79]) + "…"
}
