package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitee.com/spock2300/vmake/pkg/buildscript"
	"gitee.com/spock2300/vmake/pkg/config"
	vlog "gitee.com/spock2300/vmake/pkg/log"

	"github.com/spf13/cobra"
)

var cleanAllFlag bool

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean build artifacts",
	Long: `Remove object files and build cache for all packages.
Use --all to clean build directories for all toolchains.`,
	Run: runClean,
}

func init() {
	RootCmd.AddCommand(cleanCmd)
	cleanCmd.Flags().BoolVar(&cleanAllFlag, "all", false, "clean build directories for all toolchains")
}

type pkgCleanEntry struct {
	Dir, Name string
}

func cleanPackages(entries []pkgCleanEntry, cfg *config.ConfigFile, cleanAll bool) {
	_, tcName, err := GetToolchain(cfg)
	if err != nil {
		vlog.Error("Error: %v", err)
		return
	}

	mode := resolveMode(cfg, "")

	for _, pkg := range entries {
		if cleanAll {
			cleanAllToolchains(pkg.Dir, pkg.Name)
		} else {
			cleanToolchainDir(pkg.Dir, pkg.Name, tcName, mode)
			pkgTc := resolvePkgToolchain(cfg, pkg.Name, tcName)
			if pkgTc != tcName {
				cleanToolchainDir(pkg.Dir, pkg.Name, pkgTc, mode)
			}
		}
	}

	vlog.Info("Clean completed!")
}

func scanPackages(workDir string) []pkgCleanEntry {
	packages, err := buildscript.Scan(workDir)
	fatalErr(err)

	entries := make([]pkgCleanEntry, len(packages))
	for i, pkg := range packages {
		entries[i] = pkgCleanEntry{Dir: pkg.Dir, Name: pkg.Name}
	}
	return entries
}

func runClean(cmd *cobra.Command, args []string) {
	ctx := mustInitContext()
	entries := scanPackages(ctx.WorkDir)
	cleanPackages(entries, ctx.Config, cleanAllFlag)
}

func cleanToolchainDir(dir, pkgName, tc, mode string) {
	buildDir := fmt.Sprintf("%s-%s", tc, mode)
	cleanDir(filepath.Join(dir, "build", buildDir), pkgName, buildDir)
}

func cleanDir(path, pkgName, label string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return
	}
	if err := os.RemoveAll(path); err != nil {
		vlog.Error("Failed to clean %s/%s: %v", pkgName, label, err)
		return
	}
	vlog.Info("Cleaned %s/%s/", pkgName, label)
}

func removeIfExists(path, pkgName, label string, isDir bool) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return
	}
	var err error
	if isDir {
		err = os.RemoveAll(path)
	} else {
		err = os.Remove(path)
	}
	if err != nil {
		if pkgName != "" {
			vlog.Error("Failed to clean %s/%s: %v", pkgName, label, err)
		} else {
			vlog.Error("Failed to clean %s: %v", label, err)
		}
		return
	}
	if pkgName != "" {
		vlog.Info("Cleaned %s/%s", pkgName, label)
	} else {
		vlog.Info("Cleaned %s", label)
	}
}

func cleanAllToolchains(dir, pkgName string) {
	buildBase := filepath.Join(dir, "build")
	entries, err := os.ReadDir(buildBase)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		cleanDir(filepath.Join(buildBase, name), pkgName, name)
	}
}
