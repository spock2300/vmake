package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/gitcmd"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/repo"
)

var pkgCmd = &cobra.Command{
	Use:   "pkg",
	Short: "Discover packages and manage project sources",
	Long:  `Search registered repositories for available packages and manage packages materialized in this project.`,
}

var pkgListCmd = &cobra.Command{
	Use:   "list",
	Short: "List packages materialized in this project",
	Long:  `List package working trees under .vmake_deps, including trees no longer needed by the active configuration.`,
	Run: func(cmd *cobra.Command, args []string) {
		printSourceLinks(cmd.OutOrStdout())
		listPackageSources(cmd.OutOrStdout(), getDepsDir())
	},
}

func printSourceLinks(w io.Writer) {
	local, err := config.LoadLocal(findProjectDir())
	if err != nil {
		fatalErr(err)
	}
	if len(local.Links) == 0 {
		return
	}
	names := make([]string, 0, len(local.Links))
	for name := range local.Links {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Fprintln(w, "Source links:")
	for _, name := range names {
		fmt.Fprintf(w, "  %s -> %s\n", name, local.Links[name])
	}
}

var (
	pkgSearchRepo       string
	pkgSearchDetails    bool
	pkgSearchForProject bool
	pkgSearchJSON       bool
	pkgShowForProject   bool
	pkgShowJSON         bool
)

var pkgSearchCmd = &cobra.Command{
	Use:   "search [pattern]",
	Short: "Search registered repositories for packages",
	Long: `Search package sources by repository directory layout.

Registry packages are listed from ~/.vmake/repos/<repo>/packages/.
Native packages are listed from this project's .vmake_deps/<repo>/<pkg>/src/.
Native repositories have no global catalog, so packages that are not cached in
the current project cannot be discovered. Native sub-packages are not listed
separately; they are read through their parent package.

The pattern matches repository and package names. Use --details to read each
build.go and also match descriptions, licenses and latest versions; details
require the repository or package commit to be trusted and are skipped with a
warning otherwise.

--for-project adds cached, locked and installed status. Installed status comes
from the default <project>/install/manifest.json; packages installed with a
custom --prefix are not detected.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPkgSearch,
}

var pkgShowCmd = &cobra.Command{
	Use:   "show <repo/pkg>[@version]",
	Short: "Show package metadata",
	Long: `Show package metadata from its build.go.

Registry packages are read from the repository checkout. Native packages must
already be cached in this project's .vmake_deps. Remote scripts run only when
their repository or package commit is trusted; otherwise only directory and
trust status are shown.

Requires lists dependencies discovered without configuration values; options may
make some of them conditional. Options and targets are resolved only when the
package is part of a project; use 'vmake query config' and 'vmake inspect' for
those.

Use --for-project to include cached, locked and installed state. Installed
status comes from the default <project>/install/manifest.json; packages
installed with a custom --prefix are not detected. Native sub-packages are read
through their parent package; pass the parent package name.`,
	Args: cobra.ExactArgs(1),
	RunE: runPkgShow,
}

func listPackageSources(w io.Writer, depsDir string) {
	entries, err := fs.ListDirEntries(depsDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(w, "No package sources")
			return
		}
		fatalErr(err)
	}

	if len(entries) == 0 {
		fmt.Fprintln(w, "No package sources")
		return
	}

	fmt.Fprintln(w, "Package sources:")
	for _, entry := range entries {
		repoName := entry.Name()
		repoPath := filepath.Join(depsDir, repoName)
		pkgs, err := fs.ListDirEntries(repoPath)
		if err != nil {
			continue
		}
		for _, pkg := range pkgs {
			fmt.Fprintf(w, "  %s/%s\n", repoName, pkg.Name())
		}
	}
}

var pkgCleanCmd = &cobra.Command{
	Use:   "clean <repo/name>",
	Short: "Clean package build outputs",
	Long:  `Remove the package build outputs. Use --all to also remove the package working tree.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		pkgRef := args[0]
		repoName, pkgName := mustSplitPkgRef(pkgRef)
		if strings.Contains(pkgName, "/") {
			fatalMsg("Error: sub-packages are cleaned through their parent package: %s", pkgRef)
		}
		fatalErr(invalidateBuildReport())

		sourceMgr := repo.NewSourceManager(getDepsDir(), getCacheDir()).WithSession(commandStorageLocks())
		fatalErr(sourceMgr.CleanOutputs(repo.PackageTreeKey(repoName, pkgName)))
		fmt.Printf("Cleaned outputs for '%s'\n", pkgRef)

		if pkgCleanAll {
			fatalErr(sourceMgr.CleanSource(repoName, pkgName))
			fmt.Printf("Cleaned source for '%s'\n", pkgRef)
		}
	},
}

var pkgLinkCmd = &cobra.Command{
	Use:   "link <repo/name> <path>",
	Short: "Build a package from a local working tree",
	Long: `Link a native or registry package to a local git working tree.

A linked native package loads build.go and sub-packages from the linked tree.
A linked registry package keeps its registry wrapper build.go and replaces only
the upstream source tree. Links are machine-local, stored in
.vmake/local.json, and ignore package versions, tags and version constraints.`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		commandStorageLocks()
		projectDir := findProjectDir()
		name, path := args[0], args[1]
		abs, err := pipeline.ValidateSourceLink(commandContext(), projectDir, getDepsDir(), getReposDir(), name, path)
		fatalErr(err)
		if err := ensureLocalLinkIgnored(projectDir); err != nil {
			fatalErr(err)
		}
		local, err := config.LoadLocal(projectDir)
		fatalErr(err)
		local.Links[name] = abs
		fatalErr(config.SaveLocal(projectDir, local))
		fmt.Printf("Linked %s -> %s\n", name, abs)
		fmt.Println("Build outputs remain under .vmake_deps; the package version and constraints are ignored while linked.")
	},
}

var pkgUnlinkCmd = &cobra.Command{
	Use:   "unlink <repo/name>",
	Short: "Remove a local source link",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		commandStorageLocks()
		projectDir := findProjectDir()
		name := args[0]
		local, err := config.LoadLocal(projectDir)
		fatalErr(err)
		if _, ok := local.Links[name]; !ok {
			fatalMsg("package %s is not linked", name)
		}
		delete(local.Links, name)
		fatalErr(config.SaveLocal(projectDir, local))
		fmt.Printf("Unlinked %s\n", name)
	},
}

func commandContext() context.Context {
	return context.Background()
}

func ensureLocalLinkIgnored(projectDir string) error {
	rel := filepath.ToSlash(filepath.Join(".vmake", config.LocalFileName))
	opts := iexec.RunOptions{Dir: projectDir, Quiet: true, CaptureStderr: true}
	if out, err := iexec.RunWithOptions("git", gitcmd.Args("rev-parse", "--is-inside-work-tree"), opts); err == nil &&
		strings.TrimSpace(string(out)) == "true" {
		if _, err := iexec.RunWithOptions("git", gitcmd.Args("ls-files", "--error-unmatch", rel), opts); err == nil {
			return fmt.Errorf("%s is tracked by git; add it to .gitignore before linking", rel)
		}
	}
	gitignore := filepath.Join(projectDir, ".gitignore")
	content, _ := os.ReadFile(gitignore)
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "!") {
			continue
		}
		if line == rel || line == ".vmake/" || line == ".vmake" || line == ".vmake/*" {
			return nil
		}
	}
	f, err := os.OpenFile(gitignore, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if len(content) > 0 && !strings.HasSuffix(string(content), "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	_, err = f.WriteString(rel + "\n")
	return err
}

var pkgCleanAll bool
var pkgUpdateDryRun bool

var pkgUpdateCmd = &cobra.Command{
	Use:   "update <repo/name>[@version]",
	Short: "Update package source (floats to origin/HEAD, or pins @version)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		pkgRef := args[0]
		version := ""
		if idx := strings.Index(pkgRef, "@"); idx >= 0 {
			version = pkgRef[idx+1:]
			pkgRef = pkgRef[:idx]
			if version == "" {
				fatalMsg("empty version after '@' in %s", args[0])
			}
		}

		repoName, pkgName := mustSplitPkgRef(pkgRef)
		if strings.Contains(pkgName, "/") {
			fatalMsg("Error: sub-packages are updated through their parent package: %s", pkgRef)
		}
		local, err := config.LoadLocal(findProjectDir())
		fatalErr(err)
		if path, ok := local.Links[pkgRef]; ok {
			fatalMsg("package %s is linked to %s; run 'vmake pkg unlink %s' first", pkgRef, path, pkgRef)
		}

		repoMgr := getRepoManager()
		sourceMgr := repo.NewSourceManager(getDepsDir(), getCacheDir()).WithSession(commandStorageLocks())

		pkg := newPkgRef(repoName, pkgName)
		if repoMgr.IsNative(repoName) {
			urlTemplate, err := repoMgr.GetNativeURL(repoName)
			fatalErr(err)
			pkg.SetGit(repo.ResolveNativeURL(urlTemplate, pkgName))
			vlog.Info("  %s: listing versions from %s", pkgRef, strings.Join(pkg.GitURLs(), ", "))
			tags, err := repo.ListRemoteTags(pkg.GitURLs())
			fatalErr(err)
			pkg.SetVersions(repo.FilterValidVersions(tags))
		} else {
			lock, err := repoMgr.Lock(repoName, false)
			fatalErr(err)
			defer lock.Release()
			buildGoPath, err := repoMgr.FindPackageGo(repoName, pkgName)
			fatalErr(err)
			commit, err := repo.GetCurrentCommit(repoMgr.Path(repoName))
			fatalErr(err)
			src := buildscript.Source{
				Name:   pkgRef,
				Path:   buildGoPath,
				Dir:    filepath.Dir(buildGoPath),
				Origin: api.SourceRemote,
				Repo:   repoName,
				Commit: commit,
			}
			src.Script, err = buildscript.DetectScriptOptions(src.Dir,
				repoMgr.Path(repoName))
			fatalErr(err)
			interpreted, err := buildscript.LoadBuildScriptWithTrust(src, remoteScriptTrustChecker)
			fatalErr(err)
			pkg.SetVersions(interpreted.Versions())
			pkg.SetGit(interpreted.GitURLs()...)
		}

		if version != "" {
			if pkgUpdateDryRun {
				fmt.Printf("Would materialize %s@%s into .vmake_deps\n", pkgRef, version)
				return
			}
			ref := pkg.GetRef(version)
			if ref == "" {
				ref = version
			}
			res, err := sourceMgr.EnsureSource(repo.SourceRequest{
				Key:     repo.PackageTreeKey(repoName, pkgName),
				URLs:    pkg.GitURLs(),
				Version: version,
				Ref:     ref,
			})
			fatalErr(err)
			fmt.Printf("Updated source for '%s' -> %s@%s (%s)\n", pkgRef, pkgRef, version, repo.ShortCommit(res.Commit))
			return
		}

		if pkgUpdateDryRun {
			fmt.Printf("Would fetch latest for '%s' (floating working tree)\n", pkgRef)
			return
		}
		fatalErr(sourceMgr.UpdateSource(pkg))
		fmt.Printf("Updated source for package '%s'\n", pkgRef)
	},
}

func init() {
	pkgCmd.AddCommand(pkgListCmd)
	pkgCmd.AddCommand(pkgSearchCmd)
	pkgCmd.AddCommand(pkgShowCmd)
	pkgCmd.AddCommand(pkgCleanCmd)
	pkgCmd.AddCommand(pkgUpdateCmd)
	pkgCmd.AddCommand(pkgLinkCmd)
	pkgCmd.AddCommand(pkgUnlinkCmd)
	RootCmd.AddCommand(pkgCmd)

	pkgCleanCmd.Flags().BoolVarP(&pkgCleanAll, "all", "a", false, "also clean source code")
	pkgUpdateCmd.Flags().BoolVar(&pkgUpdateDryRun, "dry-run", false, "show what would be fetched without fetching")
	pkgSearchCmd.Flags().StringVar(&pkgSearchRepo, "repo", "", "limit the search to one repository")
	pkgSearchCmd.Flags().BoolVar(&pkgSearchDetails, "details", false, "read build.go; also match description, license and version")
	pkgSearchCmd.Flags().BoolVar(&pkgSearchForProject, "for-project", false, "include cached, locked and installed status")
	pkgSearchCmd.Flags().BoolVar(&pkgSearchJSON, "json", false, "print machine-readable JSON")
	pkgShowCmd.Flags().BoolVar(&pkgShowForProject, "for-project", false, "include cached, locked and installed status")
	pkgShowCmd.Flags().BoolVar(&pkgShowJSON, "json", false, "print machine-readable JSON")

	pkgUpdateCmd.ValidArgsFunction = completePkgRef
	pkgCleanCmd.ValidArgsFunction = completePkgRef
	pkgUnlinkCmd.ValidArgsFunction = completePkgRef
	pkgShowCmd.ValidArgsFunction = completeCatalogPkgRef
	pkgSearchCmd.RegisterFlagCompletionFunc("repo", completeRepoName)
}
