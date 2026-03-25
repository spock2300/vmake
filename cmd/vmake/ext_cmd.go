package main

import (
	"fmt"
	"os"

	"gitee.com/spock2300/vmake/pkg/plugin"
	"gitee.com/spock2300/vmake/pkg/toolchain"

	"github.com/spf13/cobra"
)

var extCmd = &cobra.Command{
	Use:   "ext",
	Short: "Manage extension repositories",
	Long:  `Manage extension repositories that contain plugins for vmake.`,
}

var extAddCmd = &cobra.Command{
	Use:   "add <name> <git-url>",
	Short: "Add an extension repository",
	Args:  cobra.ExactArgs(2),
	Run:   runExtAdd,
}

var extRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove an extension repository",
	Args:  cobra.ExactArgs(1),
	Run:   runExtRemove,
}

var extListCmd = &cobra.Command{
	Use:   "list",
	Short: "List extension repositories and plugins",
	Run:   runExtList,
}

func init() {
	RootCmd.AddCommand(extCmd)
	extCmd.AddCommand(extAddCmd)
	extCmd.AddCommand(extRemoveCmd)
	extCmd.AddCommand(extListCmd)
}

func runExtAdd(cmd *cobra.Command, args []string) {
	name := args[0]
	gitURL := args[1]

	mgr := plugin.NewManager(vmakeDir)

	if err := mgr.AddRepo(name, gitURL); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Added extension repository '%s' from %s\n", name, gitURL)

	plugins, err := mgr.DiscoverPlugins()
	if err != nil {
		return
	}

	count := 0
	for _, p := range plugins {
		if p.RepoName == name {
			fmt.Printf("  Found plugin: %s\n", p.PluginName)
			count++
		}
	}

	if count > 0 {
		fmt.Printf("Discovered %d plugin(s). Restart vmake to use them.\n", count)
	}
}

func runExtRemove(cmd *cobra.Command, args []string) {
	name := args[0]

	mgr := plugin.NewManager(vmakeDir)

	if err := mgr.RemoveRepo(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Removed extension repository '%s'\n", name)
}

func runExtList(cmd *cobra.Command, args []string) {
	mgr := plugin.NewManager(vmakeDir)

	repos := mgr.ListRepos()
	if len(repos) == 0 {
		fmt.Println("No extension repositories found")
		fmt.Println("Use 'vmake ext add <name> <url>' to add one")
		return
	}

	fmt.Println("Extension repositories:")
	for _, r := range repos {
		fmt.Printf("  %s\n", r.Name)
		fmt.Printf("    URL: %s\n", r.URL)
		fmt.Printf("    Path: %s\n", r.Path)
	}

	plugins, err := mgr.DiscoverPlugins()
	if err != nil {
		return
	}

	if len(plugins) > 0 {
		fmt.Println("")
		fmt.Println("Discovered plugins:")
		for _, p := range plugins {
			fmt.Printf("  %s/%s", p.RepoName, p.PluginName)
			if p.Info != nil {
				fmt.Printf(" (%s)", p.Info.Version)
				if p.Info.Description != "" {
					fmt.Printf(" - %s", p.Info.Description)
				}
			}
			fmt.Println()
		}
	}
}

func loadPlugins() {
	mgr := plugin.NewManager(vmakeDir)

	plugins, err := mgr.DiscoverPlugins()
	if err != nil {
		return
	}

	for _, p := range plugins {
		soPath, err := mgr.CompilePlugin(p.PluginDir, false)
		if err != nil {
			continue
		}

		loaded, err := plugin.Load(soPath)
		if err != nil {
			continue
		}

		pluginCmd := &cobra.Command{
			Use:   p.PluginName,
			Short: p.Info.Description,
		}

		ctx := &plugin.Context{
			VMakeDir:    vmakeDir,
			PluginDir:   p.PluginDir,
			CommandName: p.PluginName,
			AddSubCommand: func(subCmd *cobra.Command) {
				pluginCmd.AddCommand(subCmd)
			},
			RegisterToolchain: func(name string, tc *toolchain.Toolchain) {
				toolchain.GetManager().RegisterToolchain(name, tc)
			},
			GetToolchains: func() map[string]*toolchain.Toolchain {
				tcs, _ := toolchain.GetManager().ListToolchains()
				return tcs
			},
			SetOnMissing: func(onMissing func(name string) (*toolchain.Toolchain, error)) {
				toolchain.GetManager().SetOnMissing(onMissing)
			},
			DownloadFile:   plugin.DownloadFile,
			ExtractArchive: plugin.ExtractArchive,
			RunGitLFS:      plugin.RunGitLFS,
		}

		loaded.Entry(ctx)

		RootCmd.AddCommand(pluginCmd)
	}
}
