package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spock2300/vmake/internal/flock"
	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/quiet"
	"github.com/spock2300/vmake/internal/scriptfs"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/repo"
	"github.com/spock2300/vmake/pkg/resolver"
)

type SearchOptions struct {
	Pattern    string
	Repo       string
	Details    bool
	ForProject bool
}

type Status struct {
	Cached    string `json:"cached,omitempty"`
	Locked    string `json:"locked,omitempty"`
	Installed bool   `json:"installed,omitempty"`
}

type SearchEntry struct {
	Repo        string  `json:"repo"`
	Name        string  `json:"name"`
	Source      string  `json:"source"`
	Cached      bool    `json:"cached,omitempty"`
	Latest      string  `json:"latest,omitempty"`
	License     string  `json:"license,omitempty"`
	Description string  `json:"description,omitempty"`
	Status      *Status `json:"status,omitempty"`
}

type SearchResult struct {
	SchemaVersion int           `json:"schemaVersion"`
	Packages      []SearchEntry `json:"packages"`
	Warnings      []string      `json:"warnings"`
}

type ShowResult struct {
	SchemaVersion int      `json:"schemaVersion"`
	Repo          string   `json:"repo"`
	Name          string   `json:"name"`
	Source        string   `json:"source"`
	Path          string   `json:"path,omitempty"`
	Linked        bool     `json:"linked,omitempty"`
	LinkPath      string   `json:"linkPath,omitempty"`
	Trusted       bool     `json:"trusted"`
	Version       string   `json:"version,omitempty"`
	Ref           string   `json:"ref,omitempty"`
	Latest        string   `json:"latest,omitempty"`
	Versions      []string `json:"versions,omitempty"`
	Description   string   `json:"description,omitempty"`
	License       string   `json:"license,omitempty"`
	Homepage      string   `json:"homepage,omitempty"`
	Git           []string `json:"git,omitempty"`
	Requires      []string `json:"requires,omitempty"`
	Status        *Status  `json:"status,omitempty"`
	Add           string   `json:"add,omitempty"`
	Warning       string   `json:"warning,omitempty"`
}

type StatusReader struct {
	lock      *lockfile.Lock
	installed map[string]bool
	sources   *repo.SourceManager
}

type RepoAccess interface {
	Exists(name string) bool
	IsNative(name string) bool
	ListPackages(repo string) ([]string, error)
	FindPackageGo(repo, name string) (string, error)
	Path(name string) string
	Lock(name string, exclusive bool) (*flock.FileLock, error)
}

type Env struct {
	Repo        RepoAccess
	Commit      func(dir string) (string, error)
	ProjectDir  string
	CacheDir    string
	RepoTrusted func(repoName string) (bool, error)
	LoadScript  func(buildscript.Source) (*api.Package, error)
}

func NewStatusReaderAt(projectDir, depsDir, cacheDir string, installed map[string]bool) (*StatusReader, error) {
	lock, err := lockfile.Load(filepath.Join(projectDir, ".vmake", lockfile.LockfileName))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load lockfile: %w", err)
	}
	if installed == nil {
		installed = map[string]bool{}
	}
	return &StatusReader{
		lock:      lock,
		installed: installed,
		sources:   repo.NewSourceManager(depsDir, cacheDir),
	}, nil
}

func (r *StatusReader) ForPackage(repoName, pkgName string) *Status {
	if r == nil {
		return nil
	}
	fullName := repoName + "/" + pkgName
	status := &Status{}
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
		if locked, ok := r.lock.GetContainer(fullName); ok {
			status.Locked = locked.Version
			if status.Locked == "" {
				status.Locked = repo.ShortCommit(locked.Commit)
			}
		} else if locked, ok := r.lock.GetSource(fullName); ok {
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

func StatusText(status *Status) string {
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

func BuildPackageSource(mgr RepoAccess, repoName, pkgName, buildGo, commit string, native, linked bool) (buildscript.Source, error) {
	dir := filepath.Dir(buildGo)
	src := buildscript.Source{
		Name:   repoName + "/" + pkgName,
		Path:   buildGo,
		Dir:    dir,
		Origin: api.SourceRemote,
		Repo:   repoName,
		Commit: commit,
	}
	if linked {
		src.Link = &buildscript.SourceLink{
			Path:        dir,
			Commit:      commit,
			Native:      true,
			LocalSource: true,
		}
	}
	rootDir := dir
	if !native && !linked && mgr != nil {
		rootDir = mgr.Path(repoName)
	}
	opts, err := buildscript.DetectScriptOptions(dir, rootDir)
	if err != nil {
		return buildscript.Source{}, err
	}
	src.Script = opts
	return src, nil
}

func (e Env) commit(dir string) (string, error) {
	if e.Commit != nil {
		return e.Commit(dir)
	}
	return repo.GetCurrentCommit(dir)
}

func (e Env) loadScript(src buildscript.Source) (*api.Package, error) {
	if e.LoadScript != nil {
		return e.LoadScript(src)
	}
	return LoadPackageScript(src, e.RepoTrusted)
}

func LoadPackageScript(src buildscript.Source, repoTrusted func(repoName string) (bool, error)) (*api.Package, error) {
	restoreReadOnly := quiet.ReadOnly()
	defer restoreReadOnly()

	interpreted, err := buildscript.LoadBuildScriptWithTrust(src, func(loadSrc buildscript.Source) error {
		if repoTrusted == nil {
			return fmt.Errorf("repository %q is not trusted; run 'vmake repo trust %s'", loadSrc.Repo, loadSrc.Repo)
		}
		trusted, trustErr := repoTrusted(loadSrc.Repo)
		if trustErr != nil {
			return trustErr
		}
		if !trusted {
			return fmt.Errorf("repository %q is not trusted; run 'vmake repo trust %s'", loadSrc.Repo, loadSrc.Repo)
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

func ListNativePackagesAt(depsDir, repoName string) ([]string, error) {
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
		srcDir := filepath.Join(root, entry.Name(), "src")
		if fs.FileExists(filepath.Join(srcDir, "build.go")) || fs.FileExists(filepath.Join(srcDir, buildscript.ManifestFileName)) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func SearchRegistryRepo(env Env, repoName string, opts SearchOptions, statuses *StatusReader) ([]SearchEntry, []string) {
	mgr := env.Repo
	names, err := mgr.ListPackages(repoName)
	if err != nil {
		return nil, []string{fmt.Sprintf("%s: %v", repoName, err)}
	}

	var warnings []string
	details := false
	commit := ""
	if opts.Details {
		lock, err := mgr.Lock(repoName, false)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", repoName, err))
		} else {
			defer lock.Release()
			commit, err = env.commit(mgr.Path(repoName))
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", repoName, err))
			} else {
				trusted, err := env.RepoTrusted(repoName)
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

	entries := make([]SearchEntry, 0, len(names))
	for _, name := range names {
		entry := SearchEntry{Repo: repoName, Name: name, Source: resolver.SourceKindRegistry.String()}
		if opts.Details && details {
			if commit == "" {
				warnings = append(warnings, fmt.Sprintf("%s/%s: repository commit is unavailable", repoName, name))
			} else if buildGo, err := mgr.FindPackageGo(repoName, name); err != nil {
				warnings = append(warnings, fmt.Sprintf("%s/%s: %v", repoName, name, err))
			} else if src, err := BuildPackageSource(mgr, repoName, name, buildGo, commit, false, false); err != nil {
				warnings = append(warnings, fmt.Sprintf("%s/%s: %v", repoName, name, err))
			} else if interpreted, err := env.loadScript(src); err != nil {
				warnings = append(warnings, fmt.Sprintf("%s/%s: %v", repoName, name, err))
			} else {
				ApplySearchMetadata(&entry, interpreted, nil)
			}
		}
		if statuses != nil {
			entry.Status = statuses.ForPackage(repoName, name)
		}
		entries = append(entries, entry)
	}
	return entries, warnings
}

func SearchNativeRepo(env Env, repoName string, opts SearchOptions, statuses *StatusReader) ([]SearchEntry, []string) {
	if env.ProjectDir == "" {
		return nil, []string{fmt.Sprintf("native repository %q has no global catalog; run inside a project to see cached packages", repoName)}
	}
	depsDir := filepath.Join(env.ProjectDir, ".vmake_deps")
	names, err := ListNativePackagesAt(depsDir, repoName)
	if err != nil {
		return nil, []string{fmt.Sprintf("%s: %v", repoName, err)}
	}
	links := make(map[string]string)
	if local, loadErr := config.LoadLocal(env.ProjectDir); loadErr == nil {
		for ref, path := range local.Links {
			linkRepo, linkName, ok := api.SplitPackageRef(ref)
			if ok && linkRepo == repoName && !strings.Contains(linkName, "/") {
				links[linkName] = path
				if !ContainsString(names, linkName) {
					names = append(names, linkName)
				}
			}
		}
		sort.Strings(names)
	}

	var warnings []string
	if len(names) == 0 {
		warnings = append(warnings, fmt.Sprintf("native repository %q has no packages cached in this project; only cached native packages can be listed", repoName))
	}
	sources := repo.NewSourceManager(depsDir, env.CacheDir)
	entries := make([]SearchEntry, 0, len(names))
	for _, name := range names {
		linkPath := links[name]
		entry := SearchEntry{Repo: repoName, Name: name, Source: resolver.SourceKindNative.String(), Cached: linkPath == ""}
		if linkPath != "" {
			if opts.Details {
				commit, err := env.commit(linkPath)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("%s/%s: %v", repoName, name, err))
				} else if src, err := BuildPackageSource(env.Repo, repoName, name, filepath.Join(linkPath, "build.go"), commit, true, true); err != nil {
					warnings = append(warnings, fmt.Sprintf("%s/%s: %v", repoName, name, err))
				} else if interpreted, err := env.loadScript(src); err != nil {
					warnings = append(warnings, fmt.Sprintf("%s/%s: %v", repoName, name, err))
				} else {
					ApplySearchMetadata(&entry, interpreted, nil)
				}
			}
			if statuses != nil {
				entry.Status = statuses.ForPackage(repoName, name)
			}
			entries = append(entries, entry)
			continue
		}
		root := sources.TreeDir(repo.PackageTreeKey(repoName, name))
		state, _ := sources.ReadState(root)
		if opts.Details {
			if state == nil || state.Commit == "" {
				warnings = append(warnings, fmt.Sprintf("%s/%s: materialized state is missing", repoName, name))
			} else {
				trusted, err := env.RepoTrusted(repoName)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("%s/%s: %v", repoName, name, err))
				} else if !trusted {
					warnings = append(warnings, fmt.Sprintf("native package %q is not trusted; details omitted (run 'vmake repo trust %s')", repoName+"/"+name, repoName))
				} else {
					buildGo := filepath.Join(depsDir, repoName, name, "src", "build.go")
					if src, err := BuildPackageSource(env.Repo, repoName, name, buildGo, state.Commit, true, false); err != nil {
						warnings = append(warnings, fmt.Sprintf("%s/%s: %v", repoName, name, err))
					} else if interpreted, err := env.loadScript(src); err != nil {
						warnings = append(warnings, fmt.Sprintf("%s/%s: %v", repoName, name, err))
					} else {
						ApplySearchMetadata(&entry, interpreted, state)
					}
				}
			}
		}
		if statuses != nil {
			entry.Status = statuses.ForPackage(repoName, name)
		}
		entries = append(entries, entry)
	}
	return entries, warnings
}

func ApplySearchMetadata(entry *SearchEntry, interpreted *api.Package, state *repo.TreeState) {
	versions := SortedPackageVersions(interpreted)
	if len(versions) == 0 && state != nil && state.Version != "" {
		versions = []string{state.Version}
	}
	if len(versions) > 0 {
		entry.Latest = versions[0]
	}
	entry.License = interpreted.License()
	entry.Description = interpreted.Description()
}

func EntryMatches(entry SearchEntry, pattern string) bool {
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

func SortedPackageVersions(pkg *api.Package) []string {
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

func FormatPackageRequires(pkg *api.Package) []string {
	seen := make(map[string]bool)
	var result []string
	for _, require := range pkg.GetRequires().Get() {
		value := require.Name
		if seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func AddRequiresSnippet(repoName, pkgName, version string) string {
	ref := repoName + "/" + pkgName
	return fmt.Sprintf("p.AddRequires(%q)", ref)
}

func TrustWarning(repoName, source string) string {
	if source == resolver.SourceKindNative.String() {
		return fmt.Sprintf("native repository %q is not trusted; run 'vmake repo trust %s'", repoName, repoName)
	}
	return fmt.Sprintf("build.go is not trusted; run 'vmake repo trust %s'", repoName)
}

type Catalog struct {
	env Env
}

func Open(env Env) *Catalog { return &Catalog{env: env} }

func (c *Catalog) Search(repoName string, opts SearchOptions, statuses *StatusReader) ([]SearchEntry, []string) {
	if c.env.Repo.IsNative(repoName) {
		return SearchNativeRepo(c.env, repoName, opts, statuses)
	}
	return SearchRegistryRepo(c.env, repoName, opts, statuses)
}

func (c *Catalog) Show(repoName, pkgName, version string) (*ShowResult, bool, error) {
	return ShowPackage(c.env, repoName, pkgName, version)
}

func (c *Catalog) Script(src buildscript.Source) (*api.Package, error) {
	return c.env.loadScript(src)
}

func ShowPackage(env Env, repoName, pkgName, version string) (*ShowResult, bool, error) {
	mgr := env.Repo
	if !mgr.Exists(repoName) {
		return nil, true, fmt.Errorf("repository %q not found; run 'vmake repo list'", repoName)
	}

	result := &ShowResult{
		SchemaVersion: 1,
		Repo:          repoName,
		Name:          pkgName,
	}
	if env.ProjectDir != "" {
		if local, loadErr := config.LoadLocal(env.ProjectDir); loadErr == nil {
			if link, ok := local.Links[repoName+"/"+pkgName]; ok {
				result.Linked = true
				result.LinkPath = link
			}
		}
	}
	var buildGo string
	var commit string
	var state *repo.TreeState
	var interpreted *api.Package
	versionsKnown := false
	linked := false
	native := mgr.IsNative(repoName)
	if native {
		result.Source = resolver.SourceKindNative.String()
		if env.ProjectDir == "" {
			return nil, true, fmt.Errorf("native package %q is project-local; run inside a vmake project", repoName+"/"+pkgName)
		}
		if local, loadErr := config.LoadLocal(env.ProjectDir); loadErr == nil {
			if link, ok := local.Links[repoName+"/"+pkgName]; ok {
				linked = true
				buildGo = filepath.Join(link, "build.go")
				if !fs.FileExists(buildGo) {
					return nil, false, fmt.Errorf("linked native package '%s/%s' has no build.go in %s", repoName, pkgName, link)
				}
				commit, loadErr = env.commit(link)
				if loadErr != nil {
					return nil, false, loadErr
				}
			}
		}
		if !linked {
			depsDir := filepath.Join(env.ProjectDir, ".vmake_deps")
			sources := repo.NewSourceManager(depsDir, env.CacheDir)
			root := sources.TreeDir(repo.PackageTreeKey(repoName, pkgName))
			state, _ = sources.ReadState(root)
			if state == nil || state.Commit == "" {
				return nil, false, fmt.Errorf("package '%s/%s' is not cached in this project; run 'vmake deps fetch %s/%s' or 'vmake build' first", repoName, pkgName, repoName, pkgName)
			}
			buildGo = filepath.Join(root, "src", "build.go")
			if !fs.FileExists(buildGo) {
				return nil, false, fmt.Errorf("package '%s/%s' has no cached build.go; run 'vmake deps fetch %s/%s' or 'vmake build' first", repoName, pkgName, repoName, pkgName)
			}
			commit = state.Commit
		}
	} else {
		result.Source = resolver.SourceKindRegistry.String()
		lock, err := mgr.Lock(repoName, false)
		if err != nil {
			return nil, false, err
		}
		defer lock.Release()
		buildGo, err = mgr.FindPackageGo(repoName, pkgName)
		if err != nil {
			return nil, false, err
		}
		commit, err = env.commit(mgr.Path(repoName))
		if err != nil {
			return nil, false, err
		}
	}

	result.Path = buildGo
	if linked {
		result.Trusted = true
	} else if env.RepoTrusted != nil {
		trusted, err := env.RepoTrusted(repoName)
		if err != nil {
			result.Warning = fmt.Sprintf("cannot verify trust: %v", err)
		} else {
			result.Trusted = trusted
		}
	}
	if result.Trusted {
		src, err := BuildPackageSource(mgr, repoName, pkgName, buildGo, commit, native, linked)
		if err != nil {
			return nil, false, fmt.Errorf("script options for %s: %w", filepath.Dir(buildGo), err)
		}
		interpreted, err = env.loadScript(src)
		if err != nil {
			return nil, false, fmt.Errorf("read metadata from %s: %w", buildGo, err)
		}
		result.Description = interpreted.Description()
		result.License = interpreted.License()
		result.Homepage = interpreted.Homepage()
		result.Git = interpreted.GitURLs()
		result.Requires = FormatPackageRequires(interpreted)
		result.Versions = SortedPackageVersions(interpreted)
		versionsKnown = len(result.Versions) > 0
	}
	if len(result.Versions) == 0 && state != nil && state.Version != "" {
		result.Versions = []string{state.Version}
	}
	if len(result.Versions) > 0 {
		result.Latest = result.Versions[0]
	}
	if !result.Trusted && result.Warning == "" {
		result.Warning = TrustWarning(repoName, result.Source)
	}

	if version != "" {
		if versionsKnown && !ContainsString(result.Versions, version) {
			return nil, true, fmt.Errorf("version %q of %s is not available; versions: %s", version, repoName+"/"+pkgName, strings.Join(result.Versions, ", "))
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
	result.Add = AddRequiresSnippet(repoName, pkgName, chosenVersion)
	return result, false, nil
}

func ContainsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
