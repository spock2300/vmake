package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	repoCmd.AddCommand(repoAddCmd)
	repoCmd.AddCommand(repoRemoveCmd)
	repoCmd.AddCommand(repoListCmd)
	repoCmd.AddCommand(repoUpdateCmd)
	RootCmd.AddCommand(repoCmd)
}

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Manage package repositories",
	Long:  `Manage package repositories that contain package definitions.`,
}

var repoAddCmd = newAddCmd("add <name> <git-url>", "Add a package repository", "repository", func(name, url string) error {
	return getRepoManager().Add(name, url)
})

var repoRemoveCmd = newRemoveCmd("remove <name>", "Remove a package repository", "repository", func(name string) error {
	return getRepoManager().Remove(name)
})

var repoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all package repositories",
	Run: func(cmd *cobra.Command, args []string) {
		repos := getRepoManager().List()
		if len(repos) == 0 {
			fmt.Println("No repositories found")
			return
		}

		fmt.Println("Repositories:")
		for _, name := range repos {
			fmt.Printf("  %s\n", name)
		}
	},
}

var repoUpdateCmd = newUpdateCmd("update <name>", "Update a package repository", "repository", func(name string) error {
	return getRepoManager().Update(name)
})
