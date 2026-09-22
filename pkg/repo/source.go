package repo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/flock"
	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/pkg/api"
)

// SourceManager manages the content-addressed global source cache:
//
//	<globalDir>/<repo>/<pkg>/<version>/src   immutable per-version checkout
//	<globalDir>/<repo>/<pkg>/<version>/out   shared binary cache (build/install)
//	<globalDir>/<repo>/<pkg>/_refs           mutable clone for tag listing (native)
//	<globalDir>/<repo>/<pkg>/_head           mutable clone for `pkg update` (floats to origin/HEAD)
//	<globalDir>/_localgit/<sha256(url)>/src  shared checkouts for local SetGit packages
//	<globalDir>/_locks/<repo>_<pkg>.lock     lock files outside the guarded package dirs
//
// Version directories are never mutated once populated; clones go through a
// temporary sibling directory and an atomic rename.
type SourceManager struct {
	sourcesDir string
	globalDir  string
	locksDir   string
}

func NewSourceManager(sourcesDir, globalDir string) *SourceManager {
	return &SourceManager{
		sourcesDir: sourcesDir,
		globalDir:  globalDir,
		locksDir:   filepath.Join(globalDir, "_locks"),
	}
}

func (m *SourceManager) pkgLockFile(repo, name string) string {
	return filepath.Join(m.locksDir, repo+"_"+name+".lock")
}

func (m *SourceManager) pkgLockName(repo, name, suffix string) string {
	return repo + "_" + name + "_" + suffix + ".lock"
}

type SourceResult struct {
	LocalSrc   string
	VersionDir string
	Commit     string
}

func (m *SourceManager) localSrcPath(pkg *api.Package) string {
	return filepath.Join(m.sourcesDir, pkg.Repo, pkg.Name, "src")
}

func (m *SourceManager) versionDir(pkg *api.Package, version string) string {
	return filepath.Join(m.globalDir, pkg.Repo, pkg.Name, version)
}

func (m *SourceManager) acquireLock(pkg *api.Package) (*flock.FileLock, error) {
	return flock.Acquire(m.pkgLockFile(pkg.Repo, pkg.Name))
}

func ShortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

// EnsureSource materializes the requested version of pkg in the global cache
// and symlinks <project>/vmake_deps/<repo>/<pkg>/src (and out) to it.
func (m *SourceManager) EnsureSource(pkg *api.Package, version string) (string, error) {
	res, err := m.EnsureVersion(pkg, version, "")
	if err != nil {
		return "", err
	}
	return res.LocalSrc, nil
}

// EnsureVersion materializes the requested version of pkg in the global cache.
// When expectedCommit is non-empty and the cached checkout resolves to a
// different commit (e.g. a moved tag), the mismatch is a hard error: the
// caller must re-resolve ('vmake lock update') or purge the stale entry.
func (m *SourceManager) EnsureVersion(pkg *api.Package, version, expectedCommit string) (*SourceResult, error) {
	lock, err := m.acquireLock(pkg)
	if err != nil {
		return nil, fmt.Errorf("acquire lock for %s: %w", pkg.FullName(), err)
	}
	defer lock.Release()

	tag := pkg.GetRef(version)
	if tag == "" {
		tag = version
	}

	versionDir := m.versionDir(pkg, version)
	srcDir := filepath.Join(versionDir, "src")

	if !fs.FileExists(filepath.Join(srcDir, ".git")) {
		if err := m.materialize(pkg, versionDir, tag); err != nil {
			return nil, err
		}
	}

	commit, err := GetCurrentCommit(srcDir)
	if err != nil {
		return nil, fmt.Errorf("resolve commit for %s@%s: %w", pkg.FullName(), version, err)
	}

	if expectedCommit != "" && commit != expectedCommit {
		return nil, fmt.Errorf("cached %s@%s is commit %s but expected %s (moved tag?); run 'vmake pkg clean %s' or 'vmake lock update' to re-resolve",
			pkg.FullName(), version, ShortCommit(commit), ShortCommit(expectedCommit), pkg.FullName())
	}

	if err := m.linkProject(pkg, versionDir); err != nil {
		return nil, err
	}

	return &SourceResult{LocalSrc: m.localSrcPath(pkg), VersionDir: versionDir, Commit: commit}, nil
}

// materialize clones one of pkg's mirrors into a temporary sibling of the
// version's src directory, checks out tag, initializes submodules, then
// atomically renames it into place. versionDir itself only ever holds src/
// and out/.
func (m *SourceManager) materialize(pkg *api.Package, versionDir, tag string) error {
	srcTarget := filepath.Join(versionDir, "src")
	tmpDir := srcTarget + ".tmp"
	fs.RemoveIfExists(tmpDir)
	defer fs.RemoveIfExists(tmpDir)

	if err := m.ensureRepo(pkg, tmpDir); err != nil {
		return err
	}

	if tag != "" {
		if err := Checkout(tmpDir, tag); err != nil {
			return fmt.Errorf("checkout %s failed for %s: %w", tag, pkg.FullName(), err)
		}
	}

	if err := m.initSubmodules(pkg, tmpDir); err != nil {
		return err
	}

	if err := fs.EnsureDir(versionDir); err != nil {
		return err
	}
	fs.RemoveIfExists(srcTarget)
	if err := fs.RenameRetry(tmpDir, srcTarget); err != nil {
		return fmt.Errorf("publish %s: %w", srcTarget, err)
	}
	return nil
}

func (m *SourceManager) linkProject(pkg *api.Package, versionDir string) error {
	outDir := filepath.Join(versionDir, "out")
	if err := fs.EnsureDir(outDir); err != nil {
		return fmt.Errorf("create out for %s: %w", pkg.FullName(), err)
	}
	if err := fs.EnsureSymlink(m.localSrcPath(pkg), filepath.Join(versionDir, "src")); err != nil {
		return fmt.Errorf("link src for %s: %w", pkg.FullName(), err)
	}
	localOut := filepath.Join(m.sourcesDir, pkg.Repo, pkg.Name, "out")
	if err := fs.EnsureSymlink(localOut, outDir); err != nil {
		return fmt.Errorf("link out for %s: %w", pkg.FullName(), err)
	}
	return nil
}

// PatchSetHash summarizes a package's declared patch set. Paths are hashed
// in declaration order — patches form a series and usually do not commute,
// so EnsurePatched (which applies them in declaration order) must land on
// the same content for a given hash. It keys the patched/ cache layout and
// the remote BuildKey so different patch sets never share build outputs.
func PatchSetHash(pkg *api.Package) (string, error) {
	patches := pkg.GetPatches()
	if len(patches) == 0 {
		return "", nil
	}
	h := sha256.New()
	for _, p := range patches {
		data, err := os.ReadFile(filepath.Join(pkg.ScriptDir(), p))
		if err != nil {
			return "", fmt.Errorf("read patch %s for %s: %w", p, pkg.FullName(), err)
		}
		fmt.Fprintf(h, "%s\x00", p)
		h.Write(data)
		h.Write([]byte("\x00"))
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// EnsurePatched materializes a patched copy of the immutable version checkout
// at <versionDir>/patched/<patchHash>/src. The immutable <versionDir>/src is
// never modified; identical patch sets share one patched copy across projects.
func (m *SourceManager) EnsurePatched(pkg *api.Package, versionDir string) (string, error) {
	patchHash, err := PatchSetHash(pkg)
	if err != nil {
		return "", err
	}
	if patchHash == "" {
		return filepath.Join(versionDir, "src"), nil
	}

	patchedDir := filepath.Join(versionDir, "patched", patchHash)
	patchedSrc := filepath.Join(patchedDir, "src")

	lock, err := m.acquireLock(pkg)
	if err != nil {
		return "", fmt.Errorf("acquire lock for %s: %w", pkg.FullName(), err)
	}
	defer lock.Release()

	if fs.FileExists(filepath.Join(patchedSrc, ".git")) {
		return patchedSrc, nil
	}

	tmpDir := patchedSrc + ".tmp"
	fs.RemoveIfExists(tmpDir)
	defer fs.RemoveIfExists(tmpDir)

	if err := Clone(filepath.Join(versionDir, "src"), tmpDir); err != nil {
		return "", fmt.Errorf("clone %s for patching: %w", pkg.FullName(), err)
	}
	if err := m.initSubmodules(pkg, tmpDir); err != nil {
		return "", err
	}
	for _, p := range pkg.GetPatches() {
		if err := ApplyPatch(tmpDir, filepath.Join(pkg.ScriptDir(), p)); err != nil {
			return "", fmt.Errorf("apply patch %s for %s: %w", p, pkg.FullName(), err)
		}
	}
	if err := fs.EnsureDir(patchedDir); err != nil {
		return "", err
	}
	fs.RemoveIfExists(patchedSrc)
	if err := fs.RenameRetry(tmpDir, patchedSrc); err != nil {
		return "", fmt.Errorf("publish %s: %w", patchedSrc, err)
	}
	return patchedSrc, nil
}

func (m *SourceManager) initSubmodules(pkg *api.Package, dir string) error {
	if !pkg.Submodules() {
		return nil
	}
	if err := InitSubmodules(dir); err != nil {
		return fmt.Errorf("init submodules for %s: %w", pkg.FullName(), err)
	}
	return nil
}

func (m *SourceManager) ensureRepo(pkg *api.Package, dir string) error {
	var lastErr error
	urls := pkg.GitURLs()
	if len(urls) == 0 {
		return fmt.Errorf("no git URL for %s", pkg.FullName())
	}
	for _, url := range urls {
		lastErr = Clone(url, dir)
		if lastErr == nil {
			return nil
		}
		fs.RemoveIfExists(dir)
	}
	return fmt.Errorf("all mirrors failed for %s: %w", pkg.FullName(), lastErr)
}

// EnsureRefsClone maintains a mutable clone used only for tag listing
// (native package version discovery). Returns its path. With refresh=false it
// only clones when the refs dir is missing (offline-safe against warm caches);
// with refresh=true it fetches tags on every call. A failed fetch attempts a
// re-clone via temp-dir + atomic swap; if that also fails the existing clone
// is kept so an offline `vmake lock update` never destroys the warm refs.
func (m *SourceManager) EnsureRefsClone(pkg *api.Package, refresh bool) (string, error) {
	refsDir := filepath.Join(m.globalDir, pkg.Repo, pkg.Name, "_refs")
	lock, err := flock.Acquire(filepath.Join(m.locksDir, m.pkgLockName(pkg.Repo, pkg.Name, "refs")))
	if err != nil {
		return "", fmt.Errorf("acquire refs lock for %s/%s: %w", pkg.Repo, pkg.Name, err)
	}
	defer lock.Release()

	if !fs.FileExists(filepath.Join(refsDir, ".git")) {
		urls := pkg.GitURLs()
		if len(urls) == 0 {
			return "", fmt.Errorf("no git URL for %s", pkg.FullName())
		}
		if err := Clone(urls[0], refsDir); err != nil {
			return "", err
		}
		return refsDir, nil
	}
	if !refresh {
		return refsDir, nil
	}
	if err := FetchTags(refsDir); err != nil {
		if rerr := m.recloneRefs(pkg, refsDir); rerr != nil {
			return "", fmt.Errorf("%w (kept existing refs clone; re-clone failed: %v)", err, rerr)
		}
	}
	return refsDir, nil
}

func (m *SourceManager) recloneRefs(pkg *api.Package, refsDir string) error {
	urls := pkg.GitURLs()
	if len(urls) == 0 {
		return fmt.Errorf("no git URL for %s", pkg.FullName())
	}
	tmpDir := refsDir + ".reclone"
	backupDir := refsDir + ".bak"
	fs.RemoveIfExists(tmpDir)
	defer fs.RemoveIfExists(tmpDir)
	if err := Clone(urls[0], tmpDir); err != nil {
		return err
	}
	fs.RemoveIfExists(backupDir)
	if err := fs.RenameRetry(refsDir, backupDir); err != nil {
		return err
	}
	if err := fs.RenameRetry(tmpDir, refsDir); err != nil {
		_ = fs.RenameRetry(backupDir, refsDir)
		return err
	}
	fs.RemoveAll(backupDir)
	return nil
}

// HasMaterializedVersion reports whether the version's immutable checkout
// already exists in the global cache. Together with a valid lock entry this
// allows fully offline resolution (no refs clone, no tag listing).
func (m *SourceManager) HasMaterializedVersion(pkg *api.Package, version string) bool {
	return fs.FileExists(filepath.Join(m.versionDir(pkg, version), "src", ".git"))
}

// EnsureURL caches a clone of an arbitrary git URL (local SetGit packages,
// no repo/package structure). The result is keyed by the URL only. Local
// filesystem URLs are refreshed to origin/HEAD on each call; remote URLs are
// cloned once and never refreshed (offline-friendly).
func (m *SourceManager) EnsureURL(url string) (string, error) {
	h := sha256.Sum256([]byte(url))
	dir := filepath.Join(m.globalDir, "_localgit", hex.EncodeToString(h[:]))
	srcDir := filepath.Join(dir, "src")

	lock, err := flock.Acquire(filepath.Join(m.locksDir, "localgit_"+hex.EncodeToString(h[:8])+".lock"))
	if err != nil {
		return "", fmt.Errorf("acquire lock for %s: %w", url, err)
	}
	defer lock.Release()

	if !fs.FileExists(filepath.Join(srcDir, ".git")) {
		tmpDir := srcDir + ".tmp"
		fs.RemoveIfExists(tmpDir)
		defer fs.RemoveIfExists(tmpDir)
		if err := Clone(url, tmpDir); err != nil {
			return "", err
		}
		if err := fs.EnsureDir(dir); err != nil {
			return "", err
		}
		fs.RemoveIfExists(srcDir)
		if err := fs.RenameRetry(tmpDir, srcDir); err != nil {
			return "", fmt.Errorf("publish %s: %w", srcDir, err)
		}
	} else if isLocalGitURL(url) {
		if err := FetchAndReset(srcDir); err != nil {
			return "", err
		}
	}
	return srcDir, nil
}

func isLocalGitURL(url string) bool {
	if strings.HasPrefix(url, "file://") {
		return true
	}
	if strings.Contains(url, "://") {
		return false
	}
	if idx := strings.Index(url, ":"); idx >= 0 && strings.Contains(url[:idx], "@") {
		return false
	}
	return true
}

// UpdateSource floats a mutable clone at origin/HEAD for `vmake pkg update`
// and re-points the project symlinks at it.
func (m *SourceManager) UpdateSource(pkg *api.Package) error {
	lock, err := m.acquireLock(pkg)
	if err != nil {
		return fmt.Errorf("acquire lock for %s: %w", pkg.FullName(), err)
	}
	defer lock.Release()

	headDir := filepath.Join(m.globalDir, pkg.Repo, pkg.Name, "_head")
	if !fs.FileExists(filepath.Join(headDir, ".git")) {
		if err := fs.EnsureDir(filepath.Dir(headDir)); err != nil {
			return err
		}
		if err := m.ensureRepo(pkg, headDir); err != nil {
			return err
		}
	} else if err := FetchAndReset(headDir); err != nil {
		return err
	}

	if err := fs.EnsureSymlink(m.localSrcPath(pkg), headDir); err != nil {
		return fmt.Errorf("link src for %s: %w", pkg.FullName(), err)
	}
	localOut := filepath.Join(m.sourcesDir, pkg.Repo, pkg.Name, "out")
	_ = os.Remove(localOut)
	return nil
}

func (m *SourceManager) CleanSource(repoName, name string) error {
	localLink := filepath.Join(m.sourcesDir, repoName, name, "src")
	_ = os.Remove(localLink)
	localOut := filepath.Join(m.sourcesDir, repoName, name, "out")
	_ = os.Remove(localOut)
	return fs.RemoveAll(filepath.Join(m.globalDir, repoName, name))
}

func (m *SourceManager) CleanVersion(repoName, name, version string) error {
	return fs.RemoveAll(filepath.Join(m.globalDir, repoName, name, version))
}
