package main

import (
	"fmt"

	"github.com/spf13/cobra"

	vlog "github.com/spock2300/vmake/pkg/log"
)

var repoAddNative bool

func init() {
	repoCmd.AddCommand(repoAddCmd)
	repoCmd.AddCommand(repoRemoveCmd)
	repoCmd.AddCommand(repoListCmd)
	repoCmd.AddCommand(repoUpdateCmd)
	repoCmd.AddCommand(repoTrustCmd)
	repoCmd.AddCommand(repoUntrustCmd)
	RootCmd.AddCommand(repoCmd)

	repoAddCmd.Flags().BoolVarP(&repoAddNative, "native", "n", false, "add a native repository (URL template with {name} placeholder)")

	repoRemoveCmd.ValidArgsFunction = completeRepoName
	repoUpdateCmd.ValidArgsFunction = completeRepoName
	repoTrustCmd.ValidArgsFunction = completeRepoName
	repoUntrustCmd.ValidArgsFunction = completeRepoName
}

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Manage package repositories",
	Long:  `Manage package repositories that contain package definitions.`,
}

var repoAddCmd = &cobra.Command{
	Use:   "add <name> <git-url-or-template>",
	Short: "Add a package repository",
	Long: `Add a package repository.
  Registry repo: vmake repo add official https://github.com/user/vmake-packages
  Native repo:   vmake repo add --native myorg https://git.example.com/{name}.git`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name, url := args[0], args[1]
		mgr := getRepoManager()
		if repoAddNative {
			fatalErr(mgr.AddNative(name, url))
			fmt.Printf("Added native repository '%s' with URL template '%s'\n", name, url)
		} else {
			fatalErr(mgr.Add(name, url))
			fmt.Printf("Added repository '%s' from %s\n", name, url)
		}
		if !isRepoTrusted(name) {
			confirmTrustRemoteRepo(name)
		}
	},
}

var repoTrustCmd = &cobra.Command{
	Use:   "trust <name>",
	Short: "Trust a repository's build.go scripts",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fatalErr(addTrustedRepo(args[0]))
		fmt.Printf("Trusted repository '%s'\n", args[0])
	},
}

var repoUntrustCmd = &cobra.Command{
	Use:   "untrust <name>",
	Short: "Revoke trust for a repository",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fatalErr(removeTrustedRepo(args[0]))
		fmt.Printf("Untrusted repository '%s'\n", args[0])
	},
}

var repoRemoveCmd = newActionCmd("remove <name>", "Remove a package repository", "Removed", "repository", func(name string) error {
	return getRepoManager().Remove(name)
})

var repoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all package repositories",
	Run: func(cmd *cobra.Command, args []string) {
		infos := getRepoManager().ListInfo()
		if len(infos) == 0 {
			fmt.Println("No repositories found")
			return
		}

		fmt.Println("Repositories:")
		for _, info := range infos {
			kind := "registry"
			if info.Native {
				kind = "native"
			}
			fmt.Printf("  %s (%s)\n", info.Name, kind)
		}
	},
}

var repoUpdateCmd = newActionCmd("update <name>", "Update a package repository", "Updated", "repository", func(name string) error {
	if err := getRepoManager().Update(name); err != nil {
		return err
	}
	if isRepoTrusted(name) {
		if err := removeTrustedRepo(name); err != nil {
			return fmt.Errorf("reset trust for updated repository %s: %w", name, err)
		}
		vlog.Info("repository '%s' updated: content changed, trust reset", name)
		vlog.Info("run 'vmake repo trust %s' (or the next build) to re-confirm the new scripts", name)
	}
	return nil
})
