package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/plugin"
	"gitee.com/spock2300/vmake/pkg/toolchain"
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

var extRemoveCmd = newActionCmd("remove <name>", "Remove an extension repository", "Removed", "repository", func(name string) error {
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

var extCleanCmd = &cobra.Command{
	Use:   "clean [name]",
	Short: "Clean plugin build artifacts",
	Long: `Clean compiled plugin artifacts (plugin.so, go.mod, go.sum).
If no name is given, all extension repositories are cleaned.`,
	Run: runExtClean,
}

func init() {
	RootCmd.AddCommand(extCmd)
	extCmd.AddCommand(extAddCmd)
	extCmd.AddCommand(extRemoveCmd)
	extCmd.AddCommand(extListCmd)
	extCmd.AddCommand(extUpdateCmd)
	extCmd.AddCommand(extCleanCmd)

	extRemoveCmd.ValidArgsFunction = completeExtRepoName
	extUpdateCmd.ValidArgsFunction = completeExtRepoName
	extCleanCmd.ValidArgsFunction = completeExtRepoName
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

func runExtClean(cmd *cobra.Command, args []string) {
	mgr := getPluginManager()

	if len(args) == 1 {
		name := args[0]
		repoPath := mgr.Path(name)
		if !mgr.Exists(name) {
			fatalMsg("extension repo '%s' not found", name)
		}
		fatalErr(mgr.CleanPlugins(repoPath))
		fmt.Printf("Cleaned plugin artifacts for '%s'\n", name)
		return
	}

	repos := mgr.ListRepos()
	if len(repos) == 0 {
		fmt.Println("No extension repositories found")
		return
	}

	for _, r := range repos {
		if err := mgr.CleanPlugins(r.Path); err != nil {
			vlog.Error("  Error cleaning '%s': %v", r.Name, err)
		} else {
			fmt.Printf("Cleaned '%s'\n", r.Name)
		}
	}
}

func loadPlugins() {
	mgr := getPluginManager()

	plugins, err := mgr.DiscoverPlugins()
	if err != nil {
		fatalErr(err)
	}

	for _, p := range plugins {
		soPath, err := mgr.CompilePlugin(p.PluginDir, false)
		if err != nil {
			os.Remove(soPath)
			vlog.Error("extension plugin '%s' compile failed: %v", p.PluginName, err)
			continue
		}

		loaded, err := plugin.Load(soPath)
		if err != nil {
			os.Remove(soPath)
			vlog.Error("extension plugin '%s' load failed: %v", p.PluginName, err)
			continue
		}

		pluginCmd := &cobra.Command{
			Use:   p.PluginName,
			Short: p.Info.Description,
		}

		repoDir := filepath.Dir(p.PluginDir)

		ctx := &plugin.Context{
			VMakeDir:    vmakeDir,
			PluginDir:   p.PluginDir,
			RepoDir:     repoDir,
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
			SetOnMissing: func(toolchainName string, onMissing func(name string) (*toolchain.Toolchain, error)) {
				toolchain.GetManager().SetOnMissing(toolchainName, onMissing)
			},
			AddGlobalCFlags: func(flags ...string) {
				toolchain.GetManager().AddGlobalCFlags(flags...)
			},
			AddGlobalCxxFlags: func(flags ...string) {
				toolchain.GetManager().AddGlobalCxxFlags(flags...)
			},
			AddGlobalLdFlags: func(flags ...string) {
				toolchain.GetManager().AddGlobalLdFlags(flags...)
			},
			RegisterToolchainsFromRepo: makeRegisterToolchainsFromRepo(p.PluginDir, repoDir),
			LoadToolchainDef:           makeLoadToolchainDef(p.PluginDir),
			DownloadFile:               plugin.DownloadFile,
			ExtractToDir:               plugin.ExtractToDir,
			RunGitLFS:                  plugin.RunGitLFS,
		}

		loaded.Entry(ctx)

		RootCmd.AddCommand(pluginCmd)
	}
}

func makeRegisterToolchainsFromRepo(pluginDir, repoDir string) func() {
	return func() {
		tcMgr := toolchain.GetManager()
		toolchainsDir := getToolchainsDir()

		defs := toolchain.ScanRepoToolchains(repoDir)
		for i := range defs {
			def := &defs[i]
			tcMgr.RegisterDef(def, toolchainsDir)

			if def.Install != nil {
				d := *def
				tcMgr.SetOnMissing(def.Name, makeAutoDownload(d, repoDir, toolchainsDir))
			}
		}
	}
}

func makeAutoDownload(def toolchain.ToolchainDef, repoDir, toolchainsDir string) func(string) (*toolchain.Toolchain, error) {
	return func(name string) (*toolchain.Toolchain, error) {
		installDir := def.InstallDir(toolchainsDir)
		installCfg := def.Install

		fmt.Printf("Auto-downloading %s...\n", def.Name)

		switch installCfg.Method {
		case "lfs":
			if err := plugin.RunGitLFS(repoDir, "pull", "--include", installCfg.File); err != nil {
				return nil, fmt.Errorf("failed to download: %w", err)
			}
			archivePath := filepath.Join(repoDir, "assets", "toolchains", installCfg.File)
			format := installCfg.Format
			if format == "" {
				format = toolchain.DetectFormat(installCfg.File)
			}
			if err := plugin.ExtractToDir(archivePath, toolchainsDir, format); err != nil {
				return nil, fmt.Errorf("failed to extract: %w", err)
			}
		case "http":
			archivePath := filepath.Join(toolchainsDir, installCfg.File)
			if err := plugin.DownloadFile(installCfg.URL, archivePath); err != nil {
				return nil, fmt.Errorf("failed to download: %w", err)
			}
			format := installCfg.Format
			if format == "" {
				format = toolchain.DetectFormat(installCfg.File)
			}
			if err := plugin.ExtractToDir(archivePath, toolchainsDir, format); err != nil {
				return nil, fmt.Errorf("failed to extract: %w", err)
			}
		default:
			return nil, fmt.Errorf("unknown install method '%s' for toolchain '%s'", installCfg.Method, name)
		}

		tc := def.ToToolchain(toolchainsDir)
		toolchain.GetManager().RegisterToolchain(def.Name, tc)

		fmt.Printf("Toolchain %s installed to %s\n", def.Name, installDir)
		return tc, nil
	}
}

func makeLoadToolchainDef(pluginDir string) func() (*toolchain.ToolchainDef, error) {
	return func() (*toolchain.ToolchainDef, error) {
		return toolchain.LoadToolchainDef(filepath.Join(pluginDir, "toolchain.json"))
	}
}
