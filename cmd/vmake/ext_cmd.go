package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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

func runExtUpdate(cmd *cobra.Command, args []string) {
	mgr := plugin.NewManager(vmakeDir)

	if len(args) == 1 {
		name := args[0]
		fmt.Printf("Updating extension repository '%s'...\n", name)
		if err := mgr.UpdateRepo(name); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
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
			fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
		}
	}
	fmt.Println("Done. Plugins will be recompiled on next run.")
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

	loadExtensionToolchains()
}

type toolchainManifestEntry struct {
	Name     string          `json:"name"`
	Version  string          `json:"version"`
	Host     string          `json:"host"`
	Prefix   string          `json:"prefix"`
	File     string          `json:"file"`
	Tools    toolchain.Tools `json:"tools"`
	CFlags   []string        `json:"cflags"`
	CxxFlags []string        `json:"cxxflags"`
	LdFlags  []string        `json:"ldflags"`
}

type toolchainManifest struct {
	Toolchains []toolchainManifestEntry `json:"toolchains"`
}

func loadExtensionToolchains() {
	reposDir := filepath.Join(vmakeDir, "extensions")
	entries, err := os.ReadDir(reposDir)
	if err != nil {
		return
	}

	mgr := toolchain.GetManager()
	toolchainsDir := filepath.Join(vmakeDir, "toolchains")

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		manifestPath := filepath.Join(reposDir, entry.Name(), "assets", "toolchains", "manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}

		var manifest toolchainManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}

		for _, tcEntry := range manifest.Toolchains {
			installPath := filepath.Join(toolchainsDir, tcEntry.Name+"-"+tcEntry.Version)
			mgr.RegisterToolchain(tcEntry.Name, buildToolchainFromManifest(&tcEntry, installPath))
		}
	}

	toolchain.SetOnToolMissing(func(name string) error {
		return handleAutoDownload(name)
	})
}

func buildToolchainFromManifest(entry *toolchainManifestEntry, installPath string) *toolchain.Toolchain {
	return &toolchain.Toolchain{
		Name:        entry.Name,
		DisplayName: entry.Name + " " + entry.Version,
		Host:        entry.Host,
		Prefix:      entry.Prefix,
		InstallPath: installPath,
		Tools:       entry.Tools,
		DefaultFlags: toolchain.DefaultFlags{
			CFlags:   entry.CFlags,
			CxxFlags: entry.CxxFlags,
			LdFlags:  entry.LdFlags,
		},
	}
}

func handleAutoDownload(name string) error {
	reposDir := filepath.Join(vmakeDir, "extensions")
	entries, err := os.ReadDir(reposDir)
	if err != nil {
		return fmt.Errorf("no extension repositories found")
	}

	toolchainsDir := filepath.Join(vmakeDir, "toolchains")

	for _, repoEntry := range entries {
		if !repoEntry.IsDir() {
			continue
		}

		repoDir := filepath.Join(reposDir, repoEntry.Name())
		manifestPath := filepath.Join(repoDir, "assets", "toolchains", "manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}

		var manifest toolchainManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}

		for _, tcEntry := range manifest.Toolchains {
			if tcEntry.Name != name {
				continue
			}

			installPath := filepath.Join(toolchainsDir, tcEntry.Name+"-"+tcEntry.Version)

			fmt.Printf("Auto-downloading %s-%s...\n", tcEntry.Name, tcEntry.Version)

			archivePath := filepath.Join(repoDir, "assets", "toolchains", tcEntry.File)
			if err := plugin.RunGitLFS(repoDir, "pull", "--include", "assets/toolchains/"+tcEntry.File); err != nil {
				return fmt.Errorf("failed to download: %w", err)
			}

			if err := plugin.ExtractArchive(archivePath, toolchainsDir); err != nil {
				return fmt.Errorf("failed to extract: %w", err)
			}

			mgr := toolchain.GetManager()
			mgr.RegisterToolchain(name, buildToolchainFromManifest(&tcEntry, installPath))

			fmt.Printf("Toolchain %s installed to %s\n", name, installPath)
			return nil
		}
	}

	return fmt.Errorf("toolchain '%s' not found in any extension repository", name)
}
