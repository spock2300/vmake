package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/pkg/config"
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
	return &cobra.Command{
		Use:   "describe [text]",
		Short: "Print or set the description of the active configuration",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := findProjectDir()
			if len(args) == 1 {
				commandStorageLocks()
				path, err := config.SetProjectDescription(root, args[0])
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Description saved to %s\n", path)
				return nil
			}
			cfg, _, err := config.LoadProject(root)
			if err != nil {
				return err
			}
			if cfg.Description != "" {
				fmt.Fprintln(cmd.OutOrStdout(), cfg.Description)
			}
			return nil
		},
	}
}

func newConfigUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "use <filename.json>",
		Short:             "Select an existing configuration in .vmake/project.json",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeConfigFilename,
		RunE: func(cmd *cobra.Command, args []string) error {
			commandStorageLocks()
			root := findProjectDir()
			name, err := config.UseProjectConfig(root, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Active configuration: %s\n", filepath.Join(root, ".vmake", name))
			return nil
		},
	}
}

func newConfigCopyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "copy <filename.json>",
		Short: "Copy the active configuration to a new file without switching",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			commandStorageLocks()
			root := findProjectDir()
			if err := config.CopyProjectConfig(root, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Configuration copied to %s\n", filepath.Join(root, ".vmake", args[0]))
			return nil
		},
	}
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
