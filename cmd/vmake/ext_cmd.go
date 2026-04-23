package main

import (
	"fmt"

	vlog "gitee.com/spock2300/vmake/pkg/log"
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

var extRemoveCmd = newRemoveCmd("remove <name>", "Remove an extension repository", "extension repository", func(name string) error {
	return getPluginManager().RemoveRepo(name)
})

var extListCmd = &cobra.Command{
	Use:   "list",
	Short: "List extension repositories and plugins",
	Run:   runExtList,
}

var extUpdateCmd = &cobra.Command{
	Use:   "update [name]",
	Short: "Update extension repositories",
	Long: `Update extension repositories by pulling latest changes.
If no name is given, all repositories are updated.`,
	Run: runExtUpdate,
}

func init() {
	RootCmd.AddCommand(extCmd)
	extCmd.AddCommand(extAddCmd)
	extCmd.AddCommand(extRemoveCmd)
	extCmd.AddCommand(extListCmd)
	extCmd.AddCommand(extUpdateCmd)

	extRemoveCmd.ValidArgsFunction = completeExtRepoName
	extUpdateCmd.ValidArgsFunction = completeExtRepoName
}

func runExtAdd(cmd *cobra.Command, args []string) {
	name := args[0]
	gitURL := args[1]

	mgr := getPluginManager()

	fatalErr(mgr.AddRepo(name, gitURL))

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

func runExtList(cmd *cobra.Command, args []string) {
	mgr := getPluginManager()

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

func runExtUpdate(cmd *cobra.Command, args []string) {
	mgr := getPluginManager()

	if len(args) == 1 {
		name := args[0]
		fmt.Printf("Updating extension repository '%s'...\n", name)
		fatalErr(mgr.UpdateRepo(name))
		fmt.Printf("Updated '%s'. Plugins will be recompiled on next run.\n", name)
		return
	}

	repos := mgr.ListRepos()
	if len(repos) == 0 {
		fmt.Println("No extension repositories found")
		return
	}

	for _, r := range repos {
		fmt.Printf("Updating '%s'...\n", r.Name)
		if err := mgr.UpdateRepo(r.Name); err != nil {
			vlog.Error("  Error: %v", err)
		}
	}
	fmt.Println("Done. Plugins will be recompiled on next run.")
}

func loadPlugins() {
	mgr := getPluginManager()

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
			AddGlobalFlags: func(cflags, cxxflags []string) {
				toolchain.GetManager().AddGlobalFlags(cflags, cxxflags)
			},
			DownloadFile:   plugin.DownloadFile,
			ExtractArchive: plugin.ExtractArchive,
			RunGitLFS:      plugin.RunGitLFS,
		}

		loaded.Entry(ctx)

		RootCmd.AddCommand(pluginCmd)
	}
}
