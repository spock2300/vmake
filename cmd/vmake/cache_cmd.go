package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/internal/storage"
	"github.com/spock2300/vmake/pkg/repo"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Inspect and remove shared git object caches",
}

var cacheListCmd = &cobra.Command{
	Use:   "list",
	Short: "List cached upstream repositories",
	Run: func(cmd *cobra.Command, args []string) {
		entries, err := repo.NewObjectCache(getCacheDir()).List()
		fatalErr(err)
		if len(entries) == 0 {
			fmt.Println("No cached repositories")
			return
		}
		for _, entry := range entries {
			lastUsed := "never"
			if !entry.LastUsed.IsZero() {
				lastUsed = entry.LastUsed.UTC().Format("2006-01-02 15:04:05")
			}
			fmt.Printf("%s\n  path: %s\n  commits: %d\n  last used: %s\n", entry.URL, entry.Path, entry.Commits, lastUsed)
		}
	},
}

var cacheRemoveCmd = &cobra.Command{
	Use:   "remove <url>",
	Short: "Remove one cached upstream repository",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		url := args[0]
		if strings.TrimSpace(url) == "" {
			fatalMsg("cache remove requires a non-empty URL")
		}
		if !yesFlag {
			ok, err := confirmCacheRemoval(url, os.Stdin)
			fatalErr(err)
			if !ok {
				return
			}
		}
		session, err := storage.Acquire("", getCacheDir(), true)
		fatalErr(err)
		defer session.Close()
		fatalErr(repo.NewObjectCache(getCacheDir()).Remove(url))
		fmt.Printf("Removed cached repository %s\n", url)
	},
}

func init() {
	RootCmd.AddCommand(cacheCmd)
	cacheCmd.AddCommand(cacheListCmd, cacheRemoveCmd)
}

func confirmCacheRemoval(url string, in *os.File) (bool, error) {
	if in == nil {
		return false, fmt.Errorf("cache remove requires --yes when stdin is unavailable")
	}
	if fi, err := in.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false, fmt.Errorf("cache remove requires --yes when stdin is not a terminal")
	}
	fmt.Printf("Remove cached repository %s? [y/N]: ", url)
	var answer string
	_, _ = fmt.Fscanln(in, &answer)
	return strings.EqualFold(strings.TrimSpace(answer), "y") || strings.EqualFold(strings.TrimSpace(answer), "yes"), nil
}
