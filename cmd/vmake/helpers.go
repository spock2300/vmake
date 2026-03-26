package main

import (
	"fmt"
	"os"

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
