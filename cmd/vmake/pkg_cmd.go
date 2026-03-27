package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/repo"

	"github.com/spf13/cobra"
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
		entries, err := readDirEntries(getPackagesDir())
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
			repoPath := filepath.Join(getPackagesDir(), repoName)
			pkgs, err := readDirEntries(repoPath)
			if err != nil {
				continue
			}
			for _, pkg := range pkgs {
				pkgName := pkg.Name()
				pkgPath := filepath.Join(repoPath, pkgName)
				versions, err := readDirEntries(pkgPath)
				if err != nil {
					continue
				}
				for _, ver := range versions {
					fmt.Printf("  %s/%s %s\n", repoName, pkgName, ver.Name())
				}
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

		repos := mgr.List()
		if len(repos) == 0 {
			fmt.Println("No repositories found. Use 'vmake repo add' to add one.")
			return
		}

		fmt.Println("Available packages:")
		for _, repoName := range repos {
			repoPath := filepath.Join(getReposDir(), repoName, "packages")
			letterDirs, err := readDirEntries(repoPath)
			if err != nil {
				continue
			}

			for _, letterDir := range letterDirs {
				pkgDir := filepath.Join(repoPath, letterDir.Name())
				pkgs, err := readDirEntries(pkgDir)
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
	},
}

var pkgCleanCmd = &cobra.Command{
	Use:   "clean <repo/name>",
	Short: "Clean package cache",
	Long:  `Clean package cache. Use --all to also clean source code.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		pkgRef := args[0]
		repoName, pkgName, ok := api.SplitPackageRef(pkgRef)
		if !ok {
			fatalMsg("Error: invalid package reference")
		}

		installer := repo.NewPackageInstaller(nil, getPackagesDir(), "")

		fatalErr(installer.CleanBuild(pkgRef))
		fmt.Printf("Cleaned cache for '%s'\n", pkgRef)

		if pkgCleanAll {
			sourceMgr := repo.NewSourceManager(getCacheDir())

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

		repoName, pkgName, ok := api.SplitPackageRef(pkgRef)
		if !ok {
			fatalMsg("Error: invalid package reference")
		}

		repoMgr := getRepoManager()
		_, err := repoMgr.FindPackage(repoName, pkgName)
		fatalErr(err)

		sourceMgr := repo.NewSourceManager(getCacheDir())
		pkg := api.NewPackage()
		pkg.SetRepo(repoName).SetName(pkgName)

		fatalErr(sourceMgr.UpdateSource(pkg))

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
}
