package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/internal/assets"
	"github.com/spock2300/vmake/internal/flock"
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

var extRemoveCmd = &cobra.Command{
	Use: "remove <name>", Short: "Remove an extension repository",
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		fatalErr(removeExtensionRepo(name))
		fmt.Printf("Removed repository '%s'\n", name)
	},
}

func removeExtensionRepo(name string) error {
	mgr := getPluginManager()
	lock, err := mgr.LockRepo(name, true)
	if err != nil {
		return err
	}
	defer lock.Release()
	if err := mgr.RemoveRepo(name); err != nil {
		return err
	}
	if err := revokeExtension(name); err != nil {
		vlog.Error("reset trust for removed extension %s: %v", name, err)
	}
	return nil
}

var extTrustCmd = &cobra.Command{
	Use: "trust <name>", Short: "Trust an extension repository at its current commit",
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fatalErr(trustExtension(args[0]))
	},
}

var extUntrustCmd = &cobra.Command{
	Use: "untrust <name>", Short: "Revoke trust for an extension repository",
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fatalErr(revokeExtension(args[0]))
		fmt.Printf("Untrusted extension '%s'\n", args[0])
	},
}

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
	extCmd.AddCommand(extAddCmd, extRemoveCmd, extListCmd, extUpdateCmd, extTrustCmd, extUntrustCmd)
	extRemoveCmd.ValidArgsFunction = completeExtRepoName
	extUpdateCmd.ValidArgsFunction = completeExtRepoName
	extTrustCmd.ValidArgsFunction = completeExtRepoName
	extUntrustCmd.ValidArgsFunction = completeExtRepoName
}

func runExtAdd(cmd *cobra.Command, args []string) {
	name, gitURL := args[0], args[1]
	mgr := getPluginManager()
	fatalErr(mgr.AddRepo(name, gitURL))
	fmt.Printf("Added extension repository '%s' from %s\n", name, gitURL)
	if confirmTrust(fmt.Sprintf("The extension repository %q provides code that vmake will execute (full system access).", name)) {
		if err := trustExtension(name); err != nil {
			vlog.Error("record trust for extension %s: %v", name, err)
			fmt.Printf("Extension '%s' remains untrusted; run 'vmake ext trust %s' after reviewing it.\n", name, name)
		}
	} else {
		fmt.Printf("Extension '%s' is untrusted; plugins and toolchains will not load until you run 'vmake ext trust %s'.\n", name, name)
	}
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
			if def.TargetOS != "" || def.TargetTriple != "" {
				fmt.Printf("    Targets:  os=%s triple=%s\n", def.TargetOS, def.TargetTriple)
			}
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
		changed, err := updateExtensionRepo(mgr, name)
		fatalErr(err)
		if changed {
			fmt.Printf("Updated '%s'. Trust was reset; review and run 'vmake ext trust %s'.\n", name, name)
		} else {
			fmt.Printf("Updated '%s'. No commit change; trust was kept.\n", name)
		}
		return
	}
	repos := mgr.ListRepos()
	if len(repos) == 0 {
		fmt.Println("No extension repositories found")
		return
	}
	changedAny := false
	for _, r := range repos {
		fmt.Printf("Updating '%s'...\n", r.Name)
		changed, err := updateExtensionRepo(mgr, r.Name)
		if err != nil {
			vlog.Error("  Error: %v", err)
		}
		changedAny = changedAny || changed
	}
	if changedAny {
		fmt.Println("Done. Trust was reset for updated repositories; review and run 'vmake ext trust <name>'.")
	} else {
		fmt.Println("Done. No trust records changed.")
	}
}

func updateExtensionRepo(mgr *plugin.Manager, name string) (bool, error) {
	lock, err := mgr.LockRepo(name, true)
	if err != nil {
		return false, err
	}
	defer lock.Release()
	before, beforeErr := mgr.Commit(name)
	err = mgr.UpdateRepoLocked(name)
	after, afterErr := mgr.Commit(name)
	changed := beforeErr != nil || afterErr != nil || after != before
	if changed {
		if revokeErr := revokeExtension(name); revokeErr != nil {
			vlog.Error("reset trust for extension %s: %v", name, revokeErr)
		}
	}
	return changed, err
}

var extensionRepoLocks []*flock.FileLock

func releaseExtensionRepoLocks() {
	for _, lock := range extensionRepoLocks {
		_ = lock.Release()
	}
	extensionRepoLocks = nil
}

func loadPlugins() {
	mgr := getPluginManager()
	tcMgr := toolchain.GetManager()
	trusted := make(map[string]bool)
	for _, repo := range mgr.ListRepos() {
		lock, err := mgr.LockRepo(repo.Name, false)
		if err != nil {
			vlog.Error("lock extension repository %s: %v", repo.Name, err)
			continue
		}
		ok, err := extensionTrusted(repo.Name)
		if err != nil {
			_ = lock.Release()
			vlog.Error("extension repository %s: %v", repo.Name, err)
			continue
		}
		if !ok {
			_ = lock.Release()
			vlog.Info("extension repository '%s' is not trusted at its current commit; plugins and toolchains were not loaded (run 'vmake ext trust %s')", repo.Name, repo.Name)
			continue
		}
		extensionRepoLocks = append(extensionRepoLocks, lock)
		trusted[repo.Name] = true
		tcMgr.RegisterRepo(repo.Path, getToolchainsDir())
	}
	plugins, err := mgr.DiscoverPlugins()
	if err != nil {
		vlog.Error("%v", err)
	}
	for _, p := range plugins {
		if !trusted[p.RepoName] {
			continue
		}
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
		registration := toolchain.NewRegistration(tcMgr)
		ctx := &plugin.Context{
			VMakeDir: vmakeDir, PluginDir: p.PluginDir,
			RepoDir: filepath.Dir(p.PluginDir), CommandName: p.PluginName,
			AddSubCommand:     func(cmd *cobra.Command) { pluginCmd.AddCommand(cmd) },
			RegisterToolchain: registration.RegisterToolchain,
			GetToolchains:     registration.GetToolchains,
			SetOnMissing:      func(name string, fn func(string) (*toolchain.Toolchain, error)) { registration.SetOnMissing(name, fn) },
			AddGlobalCFlags:   registration.AddGlobalCFlags,
			AddGlobalCxxFlags: registration.AddGlobalCxxFlags,
			AddGlobalLdFlags:  registration.AddGlobalLdFlags,
			DownloadFile:      assets.DownloadFile, ExtractToDir: assets.ExtractToDir, RunGitLFS: assets.RunGitLFS,
		}
		if err := plugin.RunMain(loaded, ctx); err != nil {
			registration.Abort()
			vlog.Error("%v", err)
			continue
		}
		if err := registration.Commit(); err != nil {
			registration.Abort()
			vlog.Error("plugin %s initialization: %v", p.PluginName, err)
			continue
		}
		RootCmd.AddCommand(pluginCmd)
	}
	for _, err := range tcMgr.ToolchainErrors() {
		vlog.Error("Unavailable toolchain: %s", formatToolchainDefinitionError(err))
	}
}
