package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"gitee.com/spock2300/vmake/pkg/plugin"
	"gitee.com/spock2300/vmake/pkg/repo"
)

func fatalErr(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func fatalMsg(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func getRepoManager() *repo.RepoManager {
	return repo.NewRepoManager(getReposDir())
}

func getPluginManager() *plugin.Manager {
	return plugin.NewManager(vmakeDir)
}

func readDirEntries(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var result []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() {
			result = append(result, entry)
		}
	}
	return result, nil
}

func newAddCmd(use, short, entityType string, addFn func(name, url string) error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			name, gitURL := args[0], args[1]
			fatalErr(addFn(name, gitURL))
			fmt.Printf("Added %s '%s' from %s\n", entityType, name, gitURL)
		},
	}
}

func newRemoveCmd(use, short, entityType string, removeFn func(name string) error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			fatalErr(removeFn(args[0]))
			fmt.Printf("Removed %s '%s'\n", entityType, args[0])
		},
	}
}

func newUpdateCmd(use, short, entityType string, updateFn func(name string) error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			fatalErr(updateFn(args[0]))
			fmt.Printf("Updated %s '%s'\n", entityType, args[0])
		},
	}
}
