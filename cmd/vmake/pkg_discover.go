package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	quietpkg "github.com/spock2300/vmake/internal/quiet"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/catalog"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/repo"
)

type pkgSearchOptions struct {
	pattern    string
	repo       string
	details    bool
	forProject bool
}

type (
	pkgStatus       = catalog.Status
	pkgSearchEntry  = catalog.SearchEntry
	pkgSearchResult = catalog.SearchResult
	pkgShowResult   = catalog.ShowResult
	pkgStatusReader = catalog.StatusReader
)

func runPkgSearch(cmd *cobra.Command, args []string) error {
	opts := pkgSearchOptions{
		repo:       pkgSearchRepo,
		details:    pkgSearchDetails,
		forProject: pkgSearchForProject,
	}
	if len(args) > 0 {
		opts.pattern = args[0]
	}
	result, err := searchPackages(opts)
	if err != nil {
		return err
	}
	if pkgSearchJSON {
		return writePkgJSON(cmd.OutOrStdout(), result)
	}
	return writePkgSearchTable(cmd.OutOrStdout(), result, opts)
}

func runPkgShow(cmd *cobra.Command, args []string) error {
	repoName, pkgName, version, err := splitPkgShowArg(args[0])
	if err != nil {
		return &usageError{err: err}
	}
	var statuses *pkgStatusReader
	if pkgShowForProject {
		statuses, err = newPkgStatusReader()
		if err != nil {
			return err
		}
	}
	result, err := loadPkgShow(repoName, pkgName, version)
	if err != nil {
		return err
	}
	if statuses != nil {
		result.Status = statuses.ForPackage(repoName, pkgName)
	}
	if pkgShowJSON {
		return writePkgJSON(cmd.OutOrStdout(), result)
	}
	return writePkgShowTable(cmd.OutOrStdout(), result)
}

func searchPackages(opts pkgSearchOptions) (*pkgSearchResult, error) {
	mgr := repo.NewRepoManager(getReposDir())
	infos := mgr.ListInfo()
	if opts.repo != "" {
		found := false
		for _, info := range infos {
			if info.Name == opts.repo {
				found = true
				break
			}
		}
		if !found {
			return nil, &usageError{err: fmt.Errorf("repository %q not found; run 'vmake repo list'", opts.repo)}
		}
	}

	var statuses *pkgStatusReader
	if opts.forProject {
		var err error
		statuses, err = newPkgStatusReader()
		if err != nil {
			return nil, err
		}
	}

	result := &pkgSearchResult{
		SchemaVersion: 1,
		Packages:      []pkgSearchEntry{},
		Warnings:      []string{},
	}
	if len(infos) == 0 {
		result.Warnings = append(result.Warnings, "no repositories found; run 'vmake repo add'")
		return result, nil
	}

	for _, info := range infos {
		if opts.repo != "" && info.Name != opts.repo {
			continue
		}
		entries, warnings := catalog.Open(catalogEnv(mgr)).Search(info.Name, catalog.SearchOptions{
			Pattern:    opts.pattern,
			Repo:       opts.repo,
			Details:    opts.details,
			ForProject: opts.forProject,
		}, statuses)
		result.Packages = append(result.Packages, entries...)
		result.Warnings = append(result.Warnings, warnings...)
	}

	filtered := make([]pkgSearchEntry, 0, len(result.Packages))
	for _, entry := range result.Packages {
		if catalog.EntryMatches(entry, opts.pattern) {
			filtered = append(filtered, entry)
		}
	}
	result.Packages = filtered
	sort.Slice(result.Packages, func(i, j int) bool {
		if result.Packages[i].Repo != result.Packages[j].Repo {
			return result.Packages[i].Repo < result.Packages[j].Repo
		}
		return result.Packages[i].Name < result.Packages[j].Name
	})
	return result, nil
}

func catalogEnv(mgr *repo.RepoManager) catalog.Env {
	return catalog.Env{
		Repo:        mgr,
		ProjectDir:  findProjectDirSoft(),
		CacheDir:    getCacheDir(),
		RepoTrusted: repoScriptTrusted,
		LoadScript:  loadPackageScript,
	}
}

func loadPackageScript(src buildscript.Source) (*api.Package, error) {
	restore := quietpkg.Enter(quietpkg.Script)
	defer restore()
	return catalog.LoadPackageScript(src, repoScriptTrusted)
}

func loadPkgShow(repoName, pkgName, version string) (*pkgShowResult, error) {
	res, isUsage, err := catalog.Open(catalogEnv(repo.NewRepoManager(getReposDir()))).Show(repoName, pkgName, version)
	if err != nil {
		if isUsage {
			return nil, &usageError{err: err}
		}
		return nil, err
	}
	return res, nil
}

func newPkgStatusReader() (*pkgStatusReader, error) {
	projectDir := findProjectDirSoft()
	if projectDir == "" {
		return nil, &usageError{err: fmt.Errorf("not inside a vmake project; --for-project requires a project")}
	}
	return newPkgStatusReaderAt(projectDir, filepath.Join(projectDir, ".vmake_deps"), getCacheDir())
}

func newPkgStatusReaderAt(projectDir, depsDir, cacheDir string) (*pkgStatusReader, error) {
	installed := map[string]bool{}
	manifest, err := pipeline.LoadInstallManifest(filepath.Join(projectDir, "install", "manifest.json"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load install manifest: %w", err)
	}
	if manifest != nil {
		for _, entry := range manifest.Packages {
			installed[entry.Name] = true
		}
	}
	return catalog.NewStatusReaderAt(projectDir, depsDir, cacheDir, installed)
}

func splitPkgShowArg(arg string) (string, string, string, error) {
	version := ""
	if idx := strings.LastIndex(arg, "@"); idx > 0 {
		version = arg[idx+1:]
		arg = arg[:idx]
		if version == "" {
			return "", "", "", fmt.Errorf("empty version after '@' in %q", arg+"@")
		}
	}
	if strings.Count(arg, "/") > 1 {
		return "", "", "", fmt.Errorf("native container members are inspected via 'vmake inspect' or their module container: %s", arg)
	}
	repoName, pkgName, ok := api.SplitPackageRef(arg)
	if !ok || !validPkgRefComponent(repoName) || !validPkgRefComponent(pkgName) {
		return "", "", "", fmt.Errorf("invalid package reference: %s", arg)
	}
	return repoName, pkgName, version, nil
}

func pkgSearchSource(entry pkgSearchEntry) string {
	if entry.Cached {
		return entry.Source + " (cached)"
	}
	return entry.Source
}

func writePkgJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func writePkgSearchTable(w io.Writer, result *pkgSearchResult, opts pkgSearchOptions) error {
	for _, warning := range result.Warnings {
		vlog.Error("Warning: %s", warning)
	}
	if len(result.Packages) == 0 {
		if opts.pattern != "" {
			fmt.Fprintf(w, "No packages matching %q.\n", opts.pattern)
		} else {
			fmt.Fprintln(w, "No packages found.")
		}
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	header := []string{"REPO", "PACKAGE", "SOURCE"}
	if opts.details {
		header = append(header, "LATEST", "LICENSE", "DESCRIPTION")
	}
	if opts.forProject {
		header = append(header, "STATUS")
	}
	fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, entry := range result.Packages {
		row := []string{entry.Repo, entry.Name, pkgSearchSource(entry)}
		if opts.details {
			row = append(row, valueOrDash(inspectOneLine(entry.Latest)), valueOrDash(inspectOneLine(entry.License)), valueOrDash(inspectOneLine(entry.Description)))
		}
		if opts.forProject {
			row = append(row, catalog.StatusText(entry.Status))
		}
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	return tw.Flush()
}

func writePkgShowTable(w io.Writer, result *pkgShowResult) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "%s/%s\n", result.Repo, result.Name)
	fmt.Fprintf(tw, "  Source\t%s\n", result.Source)
	if result.Path != "" {
		fmt.Fprintf(tw, "  Path\t%s\n", inspectOneLine(result.Path))
	}
	if result.Linked {
		fmt.Fprintf(tw, "  Linked\t%s\n", inspectOneLine(result.LinkPath))
	}
	fmt.Fprintf(tw, "  Trusted\t%s\n", yesNo(result.Trusted))
	if result.Description != "" {
		fmt.Fprintf(tw, "  Description\t%s\n", inspectOneLine(result.Description))
	}
	if result.License != "" {
		fmt.Fprintf(tw, "  License\t%s\n", inspectOneLine(result.License))
	}
	if result.Homepage != "" {
		fmt.Fprintf(tw, "  Homepage\t%s\n", inspectOneLine(result.Homepage))
	}
	if result.Version != "" {
		fmt.Fprintf(tw, "  Version\t%s\n", result.Version)
	}
	if result.Ref != "" && result.Ref != result.Version {
		fmt.Fprintf(tw, "  Ref\t%s\n", result.Ref)
	}
	if result.Latest != "" {
		fmt.Fprintf(tw, "  Latest\t%s\n", result.Latest)
	}
	if len(result.Versions) > 0 {
		fmt.Fprintf(tw, "  Versions\t%s\n", strings.Join(result.Versions, ", "))
	}
	if len(result.Git) > 0 {
		fmt.Fprintf(tw, "  Git\t%s\n", inspectOneLine(strings.Join(result.Git, ", ")))
	}
	if result.Trusted && result.Warning == "" {
		if len(result.Requires) == 0 {
			fmt.Fprintf(tw, "  Requires (discovery)\t(none)\n")
		} else {
			fmt.Fprintf(tw, "  Requires (discovery)\t%s\n", inspectOneLine(strings.Join(result.Requires, ", ")))
		}
	}
	if result.Status != nil {
		fmt.Fprintf(tw, "  Project\t%s\n", catalog.StatusText(result.Status))
	}
	if result.Add != "" {
		fmt.Fprintf(tw, "  Add\t%s\n", result.Add)
	}
	if result.Warning != "" {
		fmt.Fprintf(tw, "  Warning\t%s\n", inspectOneLine(result.Warning))
	}
	return tw.Flush()
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
