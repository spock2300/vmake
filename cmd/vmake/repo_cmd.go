package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var repoAddPrefix bool

func init() {
	repoCmd.AddCommand(repoAddCmd)
	repoCmd.AddCommand(repoRemoveCmd)
	repoCmd.AddCommand(repoListCmd)
	repoCmd.AddCommand(repoUpdateCmd)
	RootCmd.AddCommand(repoCmd)

	repoAddCmd.Flags().BoolVarP(&repoAddPrefix, "prefix", "p", false, "add a prefix repository (URL template with {name} placeholder)")

	repoRemoveCmd.ValidArgsFunction = completeRepoName
	repoUpdateCmd.ValidArgsFunction = completeRepoName
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
  Index repo:  vmake repo add official https://github.com/user/vmake-packages
  Prefix repo: vmake repo add --prefix myorg https://git.example.com/{name}.git`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name, url := args[0], args[1]
		mgr := getRepoManager()
		if repoAddPrefix {
			fatalErr(mgr.AddPrefix(name, url))
			fmt.Printf("Added prefix repository '%s' with URL template '%s'\n", name, url)
		} else {
			fatalErr(mgr.Add(name, url))
			fmt.Printf("Added repository '%s' from %s\n", name, url)
		}
	},
}

var repoRemoveCmd = newRemoveCmd("remove <name>", "Remove a package repository", "repository", func(name string) error {
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
			kind := "index"
			if info.Prefix {
				kind = "prefix"
			}
			fmt.Printf("  %s (%s)\n", info.Name, kind)
		}
	},
}

var repoUpdateCmd = newUpdateCmd("update <name>", "Update a package repository", "repository", func(name string) error {
	return getRepoManager().Update(name)
})
