package main

import (
	"fmt"
	"os"
	"path/filepath"

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
		packagesDir := filepath.Join(vmakeDir, "packages")

		entries, err := os.ReadDir(packagesDir)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("No packages installed")
				return
			}
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if len(entries) == 0 {
			fmt.Println("No packages installed")
			return
		}

		fmt.Println("Installed packages:")
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			repoName := entry.Name()
			repoPath := filepath.Join(packagesDir, repoName)
			pkgs, err := os.ReadDir(repoPath)
			if err != nil {
				continue
			}
			for _, pkg := range pkgs {
				if !pkg.IsDir() {
					continue
				}
				pkgName := pkg.Name()
				pkgPath := filepath.Join(repoPath, pkgName)
				versions, err := os.ReadDir(pkgPath)
				if err != nil {
					continue
				}
				for _, ver := range versions {
					if !ver.IsDir() {
						continue
					}
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

		reposDir := filepath.Join(vmakeDir, "repos")
		mgr := repo.NewRepoManager(reposDir)

		repos := mgr.List()
		if len(repos) == 0 {
			fmt.Println("No repositories found. Use 'vmake repo add' to add one.")
			return
		}

		fmt.Println("Available packages:")
		for _, repoName := range repos {
			repoPath := filepath.Join(reposDir, repoName, "packages")
			entries, err := os.ReadDir(repoPath)
			if err != nil {
				continue
			}

			for _, letterDir := range entries {
				if !letterDir.IsDir() {
					continue
				}
				pkgDir := filepath.Join(repoPath, letterDir.Name())
				pkgs, err := os.ReadDir(pkgDir)
				if err != nil {
					continue
				}

				for _, pkg := range pkgs {
					if !pkg.IsDir() {
						continue
					}
					fullName := repoName + "/" + pkg.Name()
					if pattern == "" || containsString(fullName, pattern) {
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
		parts := splitPackageRef(pkgRef)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "Error: invalid package reference\n")
			os.Exit(1)
		}

		packagesDir := filepath.Join(vmakeDir, "packages")
		installer := repo.NewInstaller(nil, packagesDir, "")

		if err := installer.CleanBuild(pkgRef); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Cleaned cache for '%s'\n", pkgRef)

		if pkgCleanAll {
			cacheDir := filepath.Join(vmakeDir, "cache")
			sourceMgr := repo.NewSourceManager(cacheDir)

			if err := sourceMgr.CleanSource(parts[0], parts[1]); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
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

		parts := splitPackageRef(pkgRef)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "Error: invalid package reference\n")
			os.Exit(1)
		}

		reposDir := filepath.Join(vmakeDir, "repos")
		sourcesDir := filepath.Join(vmakeDir, "cache")

		repoMgr := repo.NewRepoManager(reposDir)
		pkgPath, err := repoMgr.FindPackage(parts[0], parts[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		_ = pkgPath

		sourceMgr := repo.NewSourceManager(sourcesDir)
		pkgDef := repo.NewPackageDef(parts[0], parts[1])

		if err := sourceMgr.UpdateSource(pkgDef); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Updated source for package '%s'\n", pkgRef)
	},
}

func splitPackageRef(ref string) []string {
	for i, c := range ref {
		if c == '/' {
			return []string{ref[:i], ref[i+1:]}
		}
	}
	return nil
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func init() {
	pkgCmd.AddCommand(pkgListCmd)
	pkgCmd.AddCommand(pkgSearchCmd)
	pkgCmd.AddCommand(pkgCleanCmd)
	pkgCmd.AddCommand(pkgUpdateCmd)
	RootCmd.AddCommand(pkgCmd)

	pkgCleanCmd.Flags().BoolVarP(&pkgCleanAll, "all", "a", false, "also clean source code")
}
