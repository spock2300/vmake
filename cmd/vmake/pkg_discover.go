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

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/scriptfs"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/lockfile"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/repo"
)

type pkgSearchOptions struct {
	pattern    string
	repo       string
	details    bool
	forProject bool
}

type pkgStatus struct {
	Cached    string `json:"cached,omitempty"`
	Locked    string `json:"locked,omitempty"`
	Installed bool   `json:"installed,omitempty"`
}

type pkgSearchEntry struct {
	Repo        string     `json:"repo"`
	Name        string     `json:"name"`
	Source      string     `json:"source"`
	Cached      bool       `json:"cached,omitempty"`
	Latest      string     `json:"latest,omitempty"`
	License     string     `json:"license,omitempty"`
	Description string     `json:"description,omitempty"`
	Status      *pkgStatus `json:"status,omitempty"`
}

type pkgSearchResult struct {
	SchemaVersion int              `json:"schemaVersion"`
	Packages      []pkgSearchEntry `json:"packages"`
	Warnings      []string         `json:"warnings"`
}

type pkgShowResult struct {
	SchemaVersion int        `json:"schemaVersion"`
	Repo          string     `json:"repo"`
	Name          string     `json:"name"`
	Source        string     `json:"source"`
	Path          string     `json:"path,omitempty"`
	Trusted       bool       `json:"trusted"`
	Version       string     `json:"version,omitempty"`
	Ref           string     `json:"ref,omitempty"`
	Latest        string     `json:"latest,omitempty"`
	Versions      []string   `json:"versions,omitempty"`
	Description   string     `json:"description,omitempty"`
	License       string     `json:"license,omitempty"`
	Homepage      string     `json:"homepage,omitempty"`
	Git           []string   `json:"git,omitempty"`
	Requires      []string   `json:"requires,omitempty"`
	Status        *pkgStatus `json:"status,omitempty"`
	Add           string     `json:"add,omitempty"`
	Warning       string     `json:"warning,omitempty"`
}

type pkgStatusReader struct {
	lock      *lockfile.Lock
	installed map[string]bool
	sources   *repo.SourceManager
}

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
		result.Status = statuses.forPackage(repoName, pkgName)
	}
	if pkgShowJSON {
		return writePkgJSON(cmd.OutOrStdout(), result)
	}
	return writePkgShowTable(cmd.OutOrStdout(), result)
}

func searchPackages(opts pkgSearchOptions) (*pkgSearchResult, error) {
	mgr := getRepoManager()
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
		var entries []pkgSearchEntry
		var warnings []string
		if info.Native {
			entries, warnings = searchNativeRepo(info.Name, opts, statuses)
		} else {
			entries, warnings = searchRegistryRepo(mgr, info.Name, opts, statuses)
		}
		result.Packages = append(result.Packages, entries...)
		result.Warnings = append(result.Warnings, warnings...)
	}

	filtered := make([]pkgSearchEntry, 0, len(result.Packages))
	for _, entry := range result.Packages {
		if pkgEntryMatches(entry, opts.pattern) {
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

func searchRegistryRepo(mgr *repo.RepoManager, repoName string, opts pkgSearchOptions, statuses *pkgStatusReader) ([]pkgSearchEntry, []string) {
	names, err := mgr.ListPackages(repoName)
	if err != nil {
		return nil, []string{fmt.Sprintf("%s: %v", repoName, err)}
	}

	var warnings []string
	details := false
	commit := ""
	if opts.details {
		lock, err := mgr.Lock(repoName, false)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", repoName, err))
		} else {
			defer lock.Release()
			commit, err = repo.GetCurrentCommit(mgr.Path(repoName))
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", repoName, err))
			} else {
				trusted, err := repoScriptTrusted(repoName, commit)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("%s: %v", repoName, err))
				} else if trusted {
					details = true
				} else {
					warnings = append(warnings, fmt.Sprintf("repository %q is not trusted; package details omitted (run 'vmake repo trust %s')", repoName, repoName))
				}
			}
		}
	}

	entries := make([]pkgSearchEntry, 0, len(names))
	for _, name := range names {
		entry := pkgSearchEntry{Repo: repoName, Name: name, Source: "registry"}
		if opts.details && details {
			interpreted, err := loadRegistryPackage(mgr, repoName, name, commit)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s/%s: %v", repoName, name, err))
			} else {
				applySearchMetadata(&entry, interpreted, nil)
			}
		}
		if statuses != nil {
			entry.Status = statuses.forPackage(repoName, name)
		}
		entries = append(entries, entry)
	}
	return entries, warnings
}

func searchNativeRepo(repoName string, opts pkgSearchOptions, statuses *pkgStatusReader) ([]pkgSearchEntry, []string) {
	projectDir := findProjectDirSoft()
	if projectDir == "" {
		return nil, []string{fmt.Sprintf("native repository %q has no global catalog; run inside a project to see cached packages", repoName)}
	}
	depsDir := filepath.Join(projectDir, ".vmake_deps")
	names, err := listNativePackagesAt(depsDir, repoName)
	if err != nil {
		return nil, []string{fmt.Sprintf("%s: %v", repoName, err)}
	}

	var warnings []string
	if len(names) == 0 {
		warnings = append(warnings, fmt.Sprintf("native repository %q has no packages cached in this project; only cached native packages can be listed", repoName))
	}
	sources := repo.NewSourceManager(depsDir, getCacheDir())
	entries := make([]pkgSearchEntry, 0, len(names))
	for _, name := range names {
		entry := pkgSearchEntry{Repo: repoName, Name: name, Source: "native", Cached: true}
		root := sources.TreeDir(repo.PackageTreeKey(repoName, name))
		state, _ := sources.ReadState(root)
		if opts.details {
			if state == nil || state.Commit == "" {
				warnings = append(warnings, fmt.Sprintf("%s/%s: materialized state is missing", repoName, name))
			} else {
				trusted, err := repoScriptTrusted(repoName, state.Commit)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("%s/%s: %v", repoName, name, err))
				} else if !trusted {
					warnings = append(warnings, fmt.Sprintf("native package %q at %s is not trusted; details omitted (trust it on first build with 'vmake build --yes')", repoName+"/"+name, repo.ShortCommit(state.Commit)))
				} else {
					interpreted, err := loadNativePackage(depsDir, repoName, name, state)
					if err != nil {
						warnings = append(warnings, fmt.Sprintf("%s/%s: %v", repoName, name, err))
					} else {
						applySearchMetadata(&entry, interpreted, state)
					}
				}
			}
		}
		if statuses != nil {
			entry.Status = statuses.forPackage(repoName, name)
		}
		entries = append(entries, entry)
	}
	return entries, warnings
}

func applySearchMetadata(entry *pkgSearchEntry, interpreted *api.Package, state *repo.TreeState) {
	versions := sortedPackageVersions(interpreted)
	if len(versions) == 0 && state != nil && state.Version != "" {
		versions = []string{state.Version}
	}
	if len(versions) > 0 {
		entry.Latest = versions[0]
	}
	entry.License = interpreted.License()
	entry.Description = interpreted.Description()
}

func loadRegistryPackage(mgr *repo.RepoManager, repoName, pkgName, commit string) (*api.Package, error) {
	if commit == "" {
		return nil, fmt.Errorf("repository commit is unavailable")
	}
	buildGo, err := mgr.FindPackageGo(repoName, pkgName)
	if err != nil {
		return nil, err
	}
	return loadPackageScript(buildscript.Source{
		Name:   repoName + "/" + pkgName,
		Path:   buildGo,
		Dir:    filepath.Dir(buildGo),
		Origin: api.SourceRemote,
		Repo:   repoName,
		Commit: commit,
	})
}

func loadNativePackage(depsDir, repoName, pkgName string, state *repo.TreeState) (*api.Package, error) {
	root := filepath.Join(depsDir, repoName, pkgName)
	srcDir := filepath.Join(root, "src")
	return loadPackageScript(buildscript.Source{
		Name:   repoName + "/" + pkgName,
		Path:   filepath.Join(srcDir, "build.go"),
		Dir:    srcDir,
		Origin: api.SourceRemote,
		Repo:   repoName,
		Commit: state.Commit,
	})
}

func loadPackageScript(src buildscript.Source) (*api.Package, error) {
	restoreStdout := silenceStdout()
	restoreStderr := silenceStderr()
	defer restoreStderr()
	defer restoreStdout()
	scriptfs.SetReadOnly(true)
	defer scriptfs.SetReadOnly(false)

	interpreted, err := buildscript.LoadBuildScriptWithTrust(src, func(loadSrc buildscript.Source) error {
		trusted, trustErr := repoScriptTrusted(loadSrc.Repo, loadSrc.Commit)
		if trustErr != nil {
			return trustErr
		}
		if !trusted {
			return fmt.Errorf("repository %q script at %s is not trusted", loadSrc.Repo, repo.ShortCommit(loadSrc.Commit))
		}
		return nil
	})
	blocked := scriptfs.TakeBlockedWrites()
	if err != nil {
		return nil, err
	}
	if len(blocked) > 0 {
		if len(blocked) > 1 {
			return nil, fmt.Errorf("%s (and %d more blocked writes)", blocked[0], len(blocked)-1)
		}
		return nil, fmt.Errorf("%s", blocked[0])
	}
	return interpreted, nil
}

func listNativePackagesAt(depsDir, repoName string) ([]string, error) {
	root := filepath.Join(depsDir, repoName)
	entries, err := fs.ListDirEntries(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if fs.FileExists(filepath.Join(root, entry.Name(), "src", "build.go")) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func loadPkgShow(repoName, pkgName, version string) (*pkgShowResult, error) {
	mgr := getRepoManager()
	if !mgr.Exists(repoName) {
		return nil, &usageError{err: fmt.Errorf("repository %q not found; run 'vmake repo list'", repoName)}
	}

	result := &pkgShowResult{
		SchemaVersion: 1,
		Repo:          repoName,
		Name:          pkgName,
	}
	var buildGo string
	var commit string
	var state *repo.TreeState
	var interpreted *api.Package
	versionsKnown := false
	native := mgr.IsNative(repoName)
	if native {
		result.Source = "native"
		projectDir := findProjectDirSoft()
		if projectDir == "" {
			return nil, &usageError{err: fmt.Errorf("native package %q is project-local; run inside a vmake project", repoName+"/"+pkgName)}
		}
		depsDir := filepath.Join(projectDir, ".vmake_deps")
		sources := repo.NewSourceManager(depsDir, getCacheDir())
		root := sources.TreeDir(repo.PackageTreeKey(repoName, pkgName))
		state, _ = sources.ReadState(root)
		if state == nil || state.Commit == "" {
			return nil, fmt.Errorf("package '%s/%s' is not cached in this project; run 'vmake pkg update %s/%s' first", repoName, pkgName, repoName, pkgName)
		}
		buildGo = filepath.Join(root, "src", "build.go")
		if !fs.FileExists(buildGo) {
			return nil, fmt.Errorf("package '%s/%s' has no cached build.go; run 'vmake pkg update %s/%s' first", repoName, pkgName, repoName, pkgName)
		}
		commit = state.Commit
	} else {
		result.Source = "registry"
		lock, err := mgr.Lock(repoName, false)
		if err != nil {
			return nil, err
		}
		defer lock.Release()
		buildGo, err = mgr.FindPackageGo(repoName, pkgName)
		if err != nil {
			return nil, err
		}
		commit, err = repo.GetCurrentCommit(mgr.Path(repoName))
		if err != nil {
			return nil, err
		}
	}

	result.Path = buildGo
	trusted, err := repoScriptTrusted(repoName, commit)
	if err != nil {
		result.Warning = fmt.Sprintf("cannot verify trust: %v", err)
	} else {
		result.Trusted = trusted
	}
	if result.Trusted {
		interpreted, err = loadPackageScript(buildscript.Source{
			Name:   repoName + "/" + pkgName,
			Path:   buildGo,
			Dir:    filepath.Dir(buildGo),
			Origin: api.SourceRemote,
			Repo:   repoName,
			Commit: commit,
		})
		if err != nil {
			return nil, fmt.Errorf("read metadata from %s: %w", buildGo, err)
		}
		result.Description = interpreted.Description()
		result.License = interpreted.License()
		result.Homepage = interpreted.Homepage()
		result.Git = interpreted.GitURLs()
		result.Requires = formatPackageRequires(interpreted)
		result.Versions = sortedPackageVersions(interpreted)
		versionsKnown = len(result.Versions) > 0
	}
	if len(result.Versions) == 0 && state != nil && state.Version != "" {
		result.Versions = []string{state.Version}
	}
	if len(result.Versions) > 0 {
		result.Latest = result.Versions[0]
	}
	if !result.Trusted && result.Warning == "" {
		result.Warning = pkgTrustWarning(repoName, result.Source, commit)
	}

	if version != "" {
		if versionsKnown && !containsString(result.Versions, version) {
			return nil, &usageError{err: fmt.Errorf("version %q of %s is not available; versions: %s", version, repoName+"/"+pkgName, strings.Join(result.Versions, ", "))}
		}
		result.Version = version
		result.Ref = version
		if interpreted != nil {
			if ref := interpreted.GetRef(version); ref != "" {
				result.Ref = ref
			}
		}
	}
	chosenVersion := result.Latest
	if result.Version != "" {
		chosenVersion = result.Version
	}
	result.Add = addRequiresSnippet(repoName, pkgName, chosenVersion)
	return result, nil
}

func newPkgStatusReader() (*pkgStatusReader, error) {
	projectDir := findProjectDirSoft()
	if projectDir == "" {
		return nil, &usageError{err: fmt.Errorf("not inside a vmake project; --for-project requires a project")}
	}
	return newPkgStatusReaderAt(projectDir, filepath.Join(projectDir, ".vmake_deps"), getCacheDir())
}

func newPkgStatusReaderAt(projectDir, depsDir, cacheDir string) (*pkgStatusReader, error) {
	lock, err := lockfile.Load(filepath.Join(projectDir, ".vmake", lockfile.LockfileName))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load lockfile: %w", err)
	}
	installed := make(map[string]bool)
	manifest, err := loadInstallManifest(filepath.Join(projectDir, "install", "manifest.json"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load install manifest: %w", err)
	}
	if manifest != nil {
		for _, entry := range manifest.Packages {
			installed[entry.Name] = true
		}
	}
	return &pkgStatusReader{
		lock:      lock,
		installed: installed,
		sources:   repo.NewSourceManager(depsDir, cacheDir),
	}, nil
}

func (r *pkgStatusReader) forPackage(repoName, pkgName string) *pkgStatus {
	if r == nil {
		return nil
	}
	fullName := repoName + "/" + pkgName
	status := &pkgStatus{}
	if r.sources != nil {
		root := r.sources.TreeDir(repo.PackageTreeKey(repoName, pkgName))
		if state, _ := r.sources.ReadState(root); state != nil {
			status.Cached = state.Version
			if status.Cached == "" {
				status.Cached = repo.ShortCommit(state.Commit)
			}
		}
	}
	if r.lock != nil {
		if locked, ok := r.lock.Get(fullName); ok {
			status.Locked = locked.Version
			if status.Locked == "" {
				status.Locked = repo.ShortCommit(locked.Commit)
			}
		}
	}
	status.Installed = r.installed[fullName]
	if status.Cached == "" && status.Locked == "" && !status.Installed {
		return nil
	}
	return status
}

func pkgEntryMatches(entry pkgSearchEntry, pattern string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if pattern == "" {
		return true
	}
	for _, value := range []string{entry.Repo + "/" + entry.Name, entry.Description, entry.License, entry.Latest} {
		if strings.Contains(strings.ToLower(value), pattern) {
			return true
		}
	}
	return false
}

func sortedPackageVersions(pkg *api.Package) []string {
	versions := pkg.GetVersions()
	sort.SliceStable(versions, func(i, j int) bool {
		left, leftOK := api.ParseVersion(versions[i])
		right, rightOK := api.ParseVersion(versions[j])
		if leftOK && rightOK {
			return left.Compare(right) > 0
		}
		if leftOK != rightOK {
			return leftOK
		}
		return versions[i] > versions[j]
	})
	return versions
}

func formatPackageRequires(pkg *api.Package) []string {
	seen := make(map[string]bool)
	var result []string
	for _, require := range pkg.GetRequires().Get() {
		value := require.Name
		if require.Constraint != "" {
			value += " " + require.Constraint
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func addRequiresSnippet(repoName, pkgName, version string) string {
	ref := repoName + "/" + pkgName
	constraint := ""
	if version != "" {
		if parsed, ok := api.ParseVersion(version); ok {
			constraint = fmt.Sprintf(" >= %d.%d", parsed.Major, parsed.Minor)
		}
	}
	return fmt.Sprintf("p.AddRequires(%q)", ref+constraint)
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
		return "", "", "", fmt.Errorf("native sub-packages are read through their parent package: %s", arg)
	}
	repoName, pkgName, ok := api.SplitPackageRef(arg)
	if !ok || !validPkgRefComponent(repoName) || !validPkgRefComponent(pkgName) {
		return "", "", "", fmt.Errorf("invalid package reference: %s", arg)
	}
	return repoName, pkgName, version, nil
}

func pkgTrustWarning(repoName, source, commit string) string {
	if source == "native" {
		return fmt.Sprintf("native package commit %s is not trusted; trust it on first build with 'vmake build --yes'", repo.ShortCommit(commit))
	}
	return fmt.Sprintf("build.go is not trusted; run 'vmake repo trust %s'", repoName)
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func pkgSearchSource(entry pkgSearchEntry) string {
	if entry.Cached {
		return entry.Source + " (cached)"
	}
	return entry.Source
}

func pkgStatusText(status *pkgStatus) string {
	if status == nil {
		return "-"
	}
	var parts []string
	if status.Cached != "" {
		parts = append(parts, "cached "+status.Cached)
	}
	if status.Locked != "" {
		parts = append(parts, "locked "+status.Locked)
	}
	if status.Installed {
		parts = append(parts, "installed")
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
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
			row = append(row, pkgStatusText(entry.Status))
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
		fmt.Fprintf(tw, "  Project\t%s\n", pkgStatusText(result.Status))
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
