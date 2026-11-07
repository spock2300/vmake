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
	"github.com/spock2300/vmake/pkg/config"
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
	Long: `List package working trees under .vmake_deps, including trees no longer needed by the active configuration.

Active source links are printed first. A linked package builds directly from its
worktree, so no tag or version bump is required. Packages without a materialized
src directory are not listed under Package sources; their remaining outputs and
third-party sources stay on disk.`,
	Run: func(cmd *cobra.Command, args []string) {
		local, err := config.LoadLocal(findProjectDir())
		fatalErr(err)
		printSourceLinks(cmd.OutOrStdout(), local.Links)
		listPackageSources(cmd.OutOrStdout(), getDepsDir(), local.Links)
	},
}

func printSourceLinks(w io.Writer, links map[string]string) {
	if len(links) == 0 {
		return
	}
	names := make([]string, 0, len(links))
	for name := range links {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Fprintln(w, "Source links:")
	for _, name := range names {
		fmt.Fprintf(w, "  %s -> %s\n", name, links[name])
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
require the repository to be trusted and are skipped with a warning otherwise.

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
their repository is trusted; otherwise only directory and trust status are
shown.

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

func listPackageSources(w io.Writer, depsDir string, links map[string]string) {
	entries, err := fs.ListDirEntries(depsDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(w, "No package sources")
			return
		}
		fatalErr(err)
	}

	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		repoName := entry.Name()
		repoPath := filepath.Join(depsDir, repoName)
		pkgs, err := fs.ListDirEntries(repoPath)
		if err != nil {
			continue
		}
		for _, pkg := range pkgs {
			pkgName := repoName + "/" + pkg.Name()
			if !fs.FileExists(filepath.Join(repoPath, pkg.Name(), "src")) {
				continue
			}
			line := "  " + pkgName
			if _, linked := links[pkgName]; linked {
				line += " (linked; stale materialized source)"
			}
			lines = append(lines, line)
		}
	}

	if len(lines) == 0 {
		fmt.Fprintln(w, "No package sources")
		return
	}
	fmt.Fprintln(w, "Package sources:")
	for _, line := range lines {
		fmt.Fprintln(w, line)
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

A linked native package loads build.go, sub-packages, OnConfig options and the
flags/defines they produce from the linked tree. A linked registry package keeps
its registry wrapper build.go and replaces only the upstream source tree. Links
are machine-local, stored in .vmake/local.json, and ignore package versions,
tags and version constraints.

While linked, edit the worktree and rerun the build; uncommitted edits are
compiled, and no commit, tag, version bump or 'vmake deps update' is required.
The existing lock entry is kept unchanged until you unlink.

Linking removes any existing materialized src/state for the package under
.vmake_deps. Per-build outputs and own-Git third-party source trees are kept.
Path validation does not interpret the linked buildscript; a link later rejected
for KConfig or patches has already removed the old tree. After unlink, the next
build uses the existing lock entry when present.`,
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

		repoName, pkgName := mustSplitPkgRef(name)
		sourceMgr := repo.NewSourceManager(getDepsDir(), getCacheDir()).WithSession(commandStorageLocks())
		if err := sourceMgr.CleanMaterializedSource(repoName, pkgName); err != nil {
			fatalErr(fmt.Errorf("link saved for %s, but removing materialized source failed: %w", name, err))
		}
		fmt.Printf("Linked %s -> %s\n", name, abs)
		fmt.Println("Removed the materialized src/state for this package; build outputs and third-party sources are kept.")
		fmt.Println("The linked tree supplies the package while linked; its vmake.lock entry is ignored until 'vmake pkg unlink'.")
	},
}

var pkgUnlinkCmd = &cobra.Command{
	Use:   "unlink <repo/name>",
	Short: "Remove a local source link",
	Long: `Remove the local source link. The next build uses the package's vmake.lock
entry when present; otherwise it resolves and writes one. Untagged working-tree
HEADs are not selected after unlink. Run 'vmake deps update' only if you want
to re-resolve newer versions; project pins and version constraints still apply.`,
	Args: cobra.ExactArgs(1),
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
		fmt.Printf("Unlinked %s; the next build uses the existing vmake.lock entry when present.\n", name)
		fmt.Println("Run 'vmake deps update' only if you want to re-resolve newer versions.")
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

func init() {
	pkgCmd.AddCommand(pkgListCmd)
	pkgCmd.AddCommand(pkgSearchCmd)
	pkgCmd.AddCommand(pkgShowCmd)
	pkgCmd.AddCommand(pkgCleanCmd)
	pkgCmd.AddCommand(pkgLinkCmd)
	pkgCmd.AddCommand(pkgUnlinkCmd)
	RootCmd.AddCommand(pkgCmd)

	pkgCleanCmd.Flags().BoolVarP(&pkgCleanAll, "all", "a", false, "also clean source code")
	pkgSearchCmd.Flags().StringVar(&pkgSearchRepo, "repo", "", "limit the search to one repository")
	pkgSearchCmd.Flags().BoolVar(&pkgSearchDetails, "details", false, "read build.go; also match description, license and version")
	pkgSearchCmd.Flags().BoolVar(&pkgSearchForProject, "for-project", false, "include cached, locked and installed status")
	pkgSearchCmd.Flags().BoolVar(&pkgSearchJSON, "json", false, "print machine-readable JSON")
	pkgShowCmd.Flags().BoolVar(&pkgShowForProject, "for-project", false, "include cached, locked and installed status")
	pkgShowCmd.Flags().BoolVar(&pkgShowJSON, "json", false, "print machine-readable JSON")

	pkgCleanCmd.ValidArgsFunction = completePkgRef
	pkgUnlinkCmd.ValidArgsFunction = completePkgRef
	pkgShowCmd.ValidArgsFunction = completeCatalogPkgRef
	pkgSearchCmd.RegisterFlagCompletionFunc("repo", completeRepoName)
}
