package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/pkg/repo"
)

var pkgCmd = &cobra.Command{
	Use:   "pkg",
	Short: "Manage packages",
	Long:  `Manage third-party packages.`,
}

var pkgListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed packages",
	Run: func(cmd *cobra.Command, args []string) {
		entries, err := fs.ListDirEntries(getDepsDir())
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("No packages installed")
				return
			}
			fatalErr(err)
		}

		if len(entries) == 0 {
			fmt.Println("No packages installed")
			return
		}

		fmt.Println("Installed packages:")
		for _, entry := range entries {
			repoName := entry.Name()
			repoPath := filepath.Join(getDepsDir(), repoName)
			pkgs, err := fs.ListDirEntries(repoPath)
			if err != nil {
				continue
			}
			for _, pkg := range pkgs {
				fmt.Printf("  %s/%s\n", repoName, pkg.Name())
			}
		}
	},
}

var pkgSearchCmd = &cobra.Command{
	Use:   "search [pattern]",
	Short: "Search for packages",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		pattern := ""
		if len(args) > 0 {
			pattern = args[0]
		}

		mgr := getRepoManager()

		infos := mgr.ListInfo()
		if len(infos) == 0 {
			fmt.Println("No repositories found. Use 'vmake repo add' to add one.")
			return
		}

		fmt.Println("Available packages:")
		for _, info := range infos {
			if !info.Native {
				searchRegistryRepo(info.Name, pattern)
			} else {
				searchNativeRepo(info.Name, pattern)
			}
		}
	},
}

func searchRegistryRepo(repoName, pattern string) {
	repoPath := filepath.Join(getReposDir(), repoName, "packages")
	letterDirs, err := fs.ListDirEntries(repoPath)
	if err != nil {
		return
	}

	for _, letterDir := range letterDirs {
		pkgDir := filepath.Join(repoPath, letterDir.Name())
		pkgs, err := fs.ListDirEntries(pkgDir)
		if err != nil {
			continue
		}

		for _, pkg := range pkgs {
			fullName := repoName + "/" + pkg.Name()
			if pattern == "" || strings.Contains(fullName, pattern) {
				fmt.Printf("  %s\n", fullName)
			}
		}
	}
}

func searchNativeRepo(repoName, pattern string) {
	depsDir := filepath.Join(getDepsDir(), repoName)
	entries, err := fs.ListDirEntries(depsDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		pkgName := entry.Name()
		srcDir := filepath.Join(depsDir, pkgName, "src")
		buildGo := filepath.Join(srcDir, "build.go")
		if !fs.FileExists(buildGo) {
			continue
		}
		fullName := repoName + "/" + pkgName
		if pattern == "" || strings.Contains(fullName, pattern) {
			fmt.Printf("  %s (cached)\n", fullName)
		}
	}
}

var pkgCleanCmd = &cobra.Command{
	Use:   "clean <repo/name>",
	Short: "Clean package cache",
	Long:  `Clean package cache. Use --all to also clean source code.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		pkgRef := args[0]
		repoName, pkgName := mustSplitPkgRef(pkgRef)

		installer := repo.NewPackageInstaller(getDepsDir())

		fatalErr(installer.CleanBuild(pkgRef))
		fmt.Printf("Cleaned cache for '%s'\n", pkgRef)

		if pkgCleanAll {
			sourceMgr := repo.NewSourceManager(getDepsDir(), getSourcesDir())

			fatalErr(sourceMgr.CleanSource(repoName, pkgName))
			fmt.Printf("Cleaned source for '%s'\n", pkgRef)
		}
	},
}

var pkgCleanAll bool

var pkgUpdateCmd = &cobra.Command{
	Use:   "update <repo/name>",
	Short: "Update package source",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		pkgRef := args[0]

		repoName, pkgName := mustSplitPkgRef(pkgRef)

		repoMgr := getRepoManager()
		sourceMgr := repo.NewSourceManager(getDepsDir(), getSourcesDir())

		if repoMgr.IsNative(repoName) {
			urlTemplate, err := repoMgr.GetNativeURL(repoName)
			fatalErr(err)
			gitURL := repo.ResolveNativeURL(urlTemplate, pkgName)
			pkg := newPkgRef(repoName, pkgName)
			pkg.SetGit(gitURL)
			fatalErr(sourceMgr.UpdateSource(pkg))
		} else {
			_, err := repoMgr.FindPackage(repoName, pkgName)
			fatalErr(err)
			pkg := newPkgRef(repoName, pkgName)
			fatalErr(sourceMgr.UpdateSource(pkg))
		}

		fmt.Printf("Updated source for package '%s'\n", pkgRef)
	},
}

func init() {
	pkgCmd.AddCommand(pkgListCmd)
	pkgCmd.AddCommand(pkgSearchCmd)
	pkgCmd.AddCommand(pkgCleanCmd)
	pkgCmd.AddCommand(pkgUpdateCmd)
	RootCmd.AddCommand(pkgCmd)

	pkgCleanCmd.Flags().BoolVarP(&pkgCleanAll, "all", "a", false, "also clean source code")

	pkgUpdateCmd.ValidArgsFunction = completePkgRef
	pkgCleanCmd.ValidArgsFunction = completePkgRef
}
