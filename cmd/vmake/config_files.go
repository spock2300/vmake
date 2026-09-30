package main

import (
	"fmt"
	"path/filepath"

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
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s%s\n", marker, cfg.Name, status)
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
			names = append(names, cfg.Name)
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}
