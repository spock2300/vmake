package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/pkg/repo"

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

var repoAddCmd = &cobra.Command{
	Use:   "add <name> <git-url>",
	Short: "Add a package repository",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		gitURL := args[1]

		reposDir := filepath.Join(vmakeDir, "repos")
		mgr := repo.NewRepoManager(reposDir)

		if err := mgr.Add(name, gitURL); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Added repository '%s' from %s\n", name, gitURL)
	},
}

var repoRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a package repository",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]

		reposDir := filepath.Join(vmakeDir, "repos")
		mgr := repo.NewRepoManager(reposDir)

		if err := mgr.Remove(name); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Removed repository '%s'\n", name)
	},
}

var repoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all package repositories",
	Run: func(cmd *cobra.Command, args []string) {
		reposDir := filepath.Join(vmakeDir, "repos")
		mgr := repo.NewRepoManager(reposDir)

		repos := mgr.List()
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

var repoUpdateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Update a package repository",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]

		reposDir := filepath.Join(vmakeDir, "repos")
		mgr := repo.NewRepoManager(reposDir)

		if err := mgr.Update(name); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Updated repository '%s'\n", name)
	},
}
