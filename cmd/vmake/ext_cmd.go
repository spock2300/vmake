package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/internal/assets"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/plugin"
	"github.com/spock2300/vmake/pkg/toolchain"
)

var extCmd = &cobra.Command{
	Use: "ext", Short: "Manage extension repositories",
	Long: `Manage extension repositories that contain plugins and compiler definitions for vmake.`,
}

var extAddCmd = &cobra.Command{
	Use: "add <name> <git-url>", Short: "Add an extension repository",
	Args: cobra.ExactArgs(2), Run: runExtAdd,
}

var extRemoveCmd = newActionCmd("remove <name>", "Remove an extension repository", "Removed", "repository", func(name string) error {
	return getPluginManager().RemoveRepo(name)
})

var extListCmd = &cobra.Command{
	Use: "list", Short: "List extension repositories and plugins", Run: runExtList,
}

var extUpdateCmd = &cobra.Command{
	Use: "update [name]", Short: "Update extension repositories",
	Long: `Update extension repositories by pulling latest changes.
If no name is given, all repositories are updated.`, Run: runExtUpdate,
}

func init() {
	RootCmd.AddCommand(extCmd)
	extCmd.AddCommand(extAddCmd, extRemoveCmd, extListCmd, extUpdateCmd)
	extRemoveCmd.ValidArgsFunction = completeExtRepoName
	extUpdateCmd.ValidArgsFunction = completeExtRepoName
}

func runExtAdd(cmd *cobra.Command, args []string) {
	name, gitURL := args[0], args[1]
	mgr := getPluginManager()
	fatalErr(mgr.AddRepo(name, gitURL))
	fmt.Printf("Added extension repository '%s' from %s\n", name, gitURL)
	plugins, err := mgr.DiscoverPlugins()
	if err != nil {
		vlog.Error("%v", err)
	}
	for _, p := range plugins {
		if p.RepoName == name {
			fmt.Printf("  Found plugin: %s\n", p.PluginName)
		}
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
		fmt.Printf("  %s\n    URL: %s\n    Path: %s\n", r.Name, r.URL, r.Path)
		defs, err := toolchain.ScanRepoToolchains(r.Path)
		if err != nil {
			vlog.Error("%v", err)
		}
		for _, def := range defs {
			fmt.Printf("    Compiler: %s (%s)\n", def.Name, def.Version)
		}
	}
	plugins, err := mgr.DiscoverPlugins()
	if err != nil {
		vlog.Error("%v", err)
	}
	if len(plugins) > 0 {
		fmt.Println("\nDiscovered plugins:")
		for _, p := range plugins {
			fmt.Printf("  %s/%s (%s) - %s\n", p.RepoName, p.PluginName, p.Info.Version, p.Info.Description)
		}
	}
}

func runExtUpdate(cmd *cobra.Command, args []string) {
	mgr := getPluginManager()
	if len(args) == 1 {
		name := args[0]
		fmt.Printf("Updating extension repository '%s'...\n", name)
		fatalErr(mgr.UpdateRepo(name))
		fmt.Printf("Updated '%s'. Plugins will be reloaded on next run.\n", name)
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
	fmt.Println("Done. Plugins will be reloaded on next run.")
}

func loadPlugins() {
	mgr := getPluginManager()
	tcMgr := toolchain.GetManager()
	for _, repo := range mgr.ListRepos() {
		tcMgr.RegisterRepo(repo.Path, getToolchainsDir())
	}
	plugins, err := mgr.DiscoverPlugins()
	if err != nil {
		vlog.Error("%v", err)
	}
	for _, p := range plugins {
		if existing, _, err := RootCmd.Find([]string{p.PluginName}); err == nil && existing != RootCmd {
			vlog.Error("extension plugin %s/%s conflicts with command %s", p.RepoName, p.PluginName, existing.CommandPath())
			continue
		}
		loaded, err := plugin.Load(p.PluginDir)
		if err != nil {
			vlog.Error("extension plugin '%s' load failed: %v", p.PluginName, err)
			continue
		}
		pluginCmd := &cobra.Command{Use: p.PluginName, Short: p.Info.Description}
		addCFlags, commitCFlags := bufferPluginFlags(tcMgr.AddGlobalCFlags)
		addCxxFlags, commitCxxFlags := bufferPluginFlags(tcMgr.AddGlobalCxxFlags)
		addLdFlags, commitLdFlags := bufferPluginFlags(tcMgr.AddGlobalLdFlags)
		ctx := &plugin.Context{
			VMakeDir: vmakeDir, PluginDir: p.PluginDir,
			RepoDir: filepath.Dir(p.PluginDir), CommandName: p.PluginName,
			AddSubCommand:     func(cmd *cobra.Command) { pluginCmd.AddCommand(cmd) },
			RegisterToolchain: tcMgr.RegisterToolchain,
			GetToolchains: func() map[string]*toolchain.Toolchain {
				tcs, _ := tcMgr.ListToolchains()
				return tcs
			},
			SetOnMissing:      func(name string, fn func(string) (*toolchain.Toolchain, error)) { tcMgr.SetOnMissing(name, fn) },
			AddGlobalCFlags:   addCFlags,
			AddGlobalCxxFlags: addCxxFlags,
			AddGlobalLdFlags:  addLdFlags,
			DownloadFile:      assets.DownloadFile, ExtractToDir: assets.ExtractToDir, RunGitLFS: assets.RunGitLFS,
		}
		if err := plugin.RunMain(loaded, ctx); err != nil {
			vlog.Error("%v", err)
			continue
		}
		commitCFlags()
		commitCxxFlags()
		commitLdFlags()
		RootCmd.AddCommand(pluginCmd)
	}
	for _, err := range tcMgr.ToolchainErrors() {
		vlog.Error("Unavailable toolchain: %s", formatToolchainDefinitionError(err))
	}
}

func bufferPluginFlags(add func(...string)) (func(...string), func()) {
	var pending []string
	committed := false
	return func(flags ...string) {
			if committed {
				add(flags...)
				return
			}
			pending = append(pending, flags...)
		}, func() {
			if committed {
				return
			}
			add(pending...)
			pending = nil
			committed = true
		}
}
