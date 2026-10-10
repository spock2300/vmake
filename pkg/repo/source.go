package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/flock"
	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/jsonio"
	"github.com/spock2300/vmake/internal/storage"
	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
)

// SourceManager materializes one shallow working tree per package under the
// project dependency directory, cloning the requested ref directly from its
// git URL with depth 1. The tree survives across build configurations and is
// re-created in place when the pinned commit or patch set changes.
//
//	<depDir>/<key>/src          working tree (one per package)
//	<depDir>/<key>/state.json   materialization identity
//	<depDir>/<key>/out/...      per-build outputs (remote packages)
//	<cacheDir>/_locks/tree_*.lock   per-tree materialization locks
type SourceManager struct {
	ctx       context.Context
	depDir    string
	cacheRoot string
	locksDir  string
	session   *storage.Session
}

func NewSourceManager(depDir, cacheDir string) *SourceManager {
	return &SourceManager{
		ctx:       context.Background(),
		depDir:    depDir,
		cacheRoot: cacheDir,
		locksDir:  filepath.Join(cacheDir, "_locks"),
	}
}

func (m *SourceManager) WithContext(ctx context.Context) *SourceManager {
	if ctx == nil {
		ctx = context.Background()
	}
	m.ctx = ctx
	return m
}

func (m *SourceManager) context() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

func (m *SourceManager) WithSession(session *storage.Session) *SourceManager {
	m.session = session
	return m
}

func (m *SourceManager) acquireAccess(exclusive bool) (func(), error) {
	if err := m.context().Err(); err != nil {
		return nil, err
	}
	if m.session != nil {
		if err := m.session.CheckAccess(m.cacheRoot, exclusive); err != nil {
			return nil, err
		}
		return func() {}, nil
	}
	session, err := storage.AcquireContext(m.context(), "", m.cacheRoot, exclusive)
	if err != nil {
		return nil, err
	}
	return func() { _ = session.Close() }, nil
}

// TreeDir is the package tree root for key inside the dependency directory.
func (m *SourceManager) TreeDir(key string) string {
	return filepath.Join(m.depDir, filepath.FromSlash(key))
}

// treePath resolves a tree key and rejects keys that would escape the
// dependency directory.
func (m *SourceManager) treePath(key string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(key))
	if clean == "." || clean == "" || !filepath.IsLocal(clean) {
		return "", fmt.Errorf("invalid package tree key %q", key)
	}
	return filepath.Join(m.depDir, clean), nil
}

// TreeSrc is the working tree path for key inside the dependency directory.
func (m *SourceManager) TreeSrc(key string) string {
	return filepath.Join(m.TreeDir(key), "src")
}

// TreeState records how a package tree was materialized.
type TreeState struct {
	URLs       []string `json:"urls"`
	Version    string   `json:"version,omitempty"`
	Ref        string   `json:"ref,omitempty"`
	Commit     string   `json:"commit"`
	PatchHash  string   `json:"patchHash,omitempty"`
	Submodules bool     `json:"submodules,omitempty"`
}

func (m *SourceManager) statePath(root string) string {
	return filepath.Join(root, "state.json")
}

func (m *SourceManager) ReadState(root string) (*TreeState, error) {
	var st TreeState
	if err := jsonio.Load(m.statePath(root), &st); err != nil {
		// A missing or corrupt state file means "no materialized identity":
		// the next EnsureSource re-materializes instead of blocking forever.
		return nil, nil
	}
	return &st, nil
}

// CommitAt reports the commit of a materialized tree rooted at root, or "".
func (m *SourceManager) CommitAt(root string) string {
	st, err := m.ReadState(root)
	if err != nil || st == nil {
		return ""
	}
	srcDir := filepath.Join(root, "src")
	if !fs.FileExists(filepath.Join(srcDir, ".git")) {
		return ""
	}
	if m.treeHead(srcDir) != st.Commit {
		return ""
	}
	return st.Commit
}

// treeHead reads the checked-out commit of a working tree, or "" when the
// repository is missing or corrupt.
func (m *SourceManager) treeHead(srcDir string) string {
	out, err := runGitCmd([]string{"rev-parse", "--verify", "HEAD^{commit}"}, exec.RunOptions{
		Context: m.context(), Dir: srcDir, Quiet: true,
	})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// SourceRequest describes one materialization request. Key identifies the
// package (locks, logging); Root overrides the default <depDir>/<key> tree
// location (used for native members carrying their own repository).
// ResolveOnly relaxes the identity check to the commit: resolution reads
// build.go before patches and submodules are known, so it must reuse a tree
// materialized by the build phase instead of re-creating it.
type SourceRequest struct {
	Key         string
	Root        string
	URLs        []string
	Version     string
	Ref         string
	Commit      string
	PatchHash   string
	Submodules  bool
	Refresh     bool
	ResolveOnly bool
}

type SourceResult struct {
	Root   string
	SrcDir string
	URL    string
	Commit string
}

func (m *SourceManager) result(root, srcDir, url, commit string) *SourceResult {
	return &SourceResult{Root: root, SrcDir: srcDir, URL: url, Commit: commit}
}

// EnsureSource materializes the package tree at the requested commit (or ref)
// and returns its paths. A tree already materialized with the same commit,
// patch set and submodule setting is reused without touching the network.
func (m *SourceManager) EnsureSource(req SourceRequest) (*SourceResult, error) {
	root := req.Root
	if root == "" {
		var err error
		root, err = m.treePath(req.Key)
		if err != nil {
			return nil, err
		}
	}
	srcDir := filepath.Join(root, "src")

	release, err := m.acquireAccess(false)
	if err != nil {
		return nil, err
	}
	defer release()
	lockName := "tree_" + storage.OwnerKey(req.Key)[:16] + ".lock"
	lock, err := flock.AcquireContext(m.context(), filepath.Join(m.locksDir, lockName))
	if err != nil {
		return nil, fmt.Errorf("acquire tree lock for %s: %w", req.Key, err)
	}
	defer lock.Release()

	state, err := m.ReadState(root)
	if err != nil {
		return nil, err
	}
	reusable := func(st *TreeState) bool {
		if st == nil || (len(req.URLs) > 0 && !sameURLs(st.URLs, req.URLs)) {
			return false
		}
		if !fs.FileExists(filepath.Join(srcDir, ".git")) {
			return false
		}
		if m.treeHead(srcDir) != st.Commit {
			return false
		}
		if req.ResolveOnly {
			return true
		}
		return st.PatchHash == req.PatchHash && st.Submodules == req.Submodules
	}
	if reusable(state) {
		if req.Commit != "" && state.Commit == req.Commit {
			return m.result(root, srcDir, firstURL(state.URLs, req.URLs), state.Commit), nil
		}
		if req.Commit == "" && req.Ref != "" && state.Ref == req.Ref && state.Version == req.Version && !req.Refresh {
			return m.result(root, srcDir, firstURL(state.URLs, req.URLs), state.Commit), nil
		}
		if req.Commit == "" && req.Ref == "" && !req.Refresh {
			return m.result(root, srcDir, firstURL(state.URLs, req.URLs), state.Commit), nil
		}
	}
	if len(req.URLs) == 0 {
		return nil, fmt.Errorf("package %s has no git URL and no reusable tree", req.Key)
	}

	commit := req.Commit
	localRef := ""
	if commit == "" {
		if isCommitSHA(req.Ref) {
			commit = req.Ref
		} else {
			resolved, refName, err := ResolveRemoteRefContext(m.context(), req.URLs, req.Ref)
			if err != nil {
				if state != nil && state.Commit != "" && m.treeHead(srcDir) == state.Commit && offlineRefMatch(state, req) {
					vlog.Info("  %s: cannot reach %s (%v); keeping existing tree at %s", req.Key, strings.Join(req.URLs, ", "), err, ShortCommit(state.Commit))
					commit = state.Commit
				} else {
					return nil, fmt.Errorf("resolve source for %s: %w", req.Key, err)
				}
			} else {
				commit = resolved
				localRef = refName
			}
		}
	}

	if reusable(state) && state.Commit == commit {
		return m.result(root, srcDir, firstURL(state.URLs, req.URLs), commit), nil
	}

	url, actual, err := m.materialize(req, root, srcDir, state, commit, localRef)
	if err != nil {
		return nil, err
	}
	commit = actual

	state = &TreeState{
		URLs:       append([]string{}, req.URLs...),
		Version:    req.Version,
		Ref:        req.Ref,
		Commit:     commit,
		PatchHash:  req.PatchHash,
		Submodules: req.Submodules,
	}
	if req.ResolveOnly {
		// Resolution materializes a clean tree; the build phase re-applies
		// patches and records the applied hash afterwards.
		state.PatchHash = ""
	}
	if err := jsonio.Save(m.statePath(root), state); err != nil {
		return nil, fmt.Errorf("record tree state for %s: %w", req.Key, err)
	}
	return m.result(root, srcDir, url, commit), nil
}

func firstURL(stateURLs, fallback []string) string {
	if len(stateURLs) > 0 {
		return stateURLs[0]
	}
	if len(fallback) > 0 {
		return fallback[0]
	}
	return ""
}

// sameURLs reports whether two ordered URL lists name the same origins. The
// URL list is part of a tree's identity: reusing a checkout after the
// declared upstream changed would silently build the wrong source.
func sameURLs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// offlineRefMatch reports whether an existing tree's recorded identity may
// stand in for the request when the remote is unreachable. A changed URL list
// means the recorded tree may not be the requested source.
func offlineRefMatch(state *TreeState, req SourceRequest) bool {
	if len(req.URLs) > 0 && !sameURLs(state.URLs, req.URLs) {
		return false
	}
	if req.Ref == "" {
		return true
	}
	return state.Ref == req.Ref && state.Version == req.Version
}

// materialize replaces the working tree with a fresh checkout of commit,
// cloning depth 1 from the first reachable URL. A tree already at the target
// commit from the same URLs is restored in place (reset + clean) instead,
// which keeps the resolver-to-build handoff for patched packages off the
// network; a changed upstream URL always re-clones.
func (m *SourceManager) materialize(req SourceRequest, root, srcDir string, state *TreeState, commit, localRef string) (string, string, error) {
	if err := m.context().Err(); err != nil {
		return "", "", err
	}
	if m.treeHead(srcDir) == commit && (state == nil || len(req.URLs) == 0 || sameURLs(state.URLs, req.URLs)) {
		_ = os.Remove(m.statePath(root))
		if err := m.restoreTree(req, srcDir, commit); err != nil {
			return "", "", err
		}
		return firstURL(stateURLs(state), req.URLs), commit, nil
	}

	tmp := srcDir + ".tmp"
	fs.RemoveIfExists(tmp)
	defer fs.RemoveIfExists(tmp)

	var lastErr error
	for _, url := range req.URLs {
		if err := m.context().Err(); err != nil {
			return "", "", err
		}
		fs.RemoveIfExists(tmp)
		actual, err := m.cloneTreeAt(url, tmp, req, commit, localRef)
		if err != nil {
			if ctxErr := m.context().Err(); ctxErr != nil {
				return "", "", ctxErr
			}
			lastErr = err
			continue
		}
		if req.Submodules {
			if err := InitSubmodulesContext(m.context(), tmp); err != nil {
				if ctxErr := m.context().Err(); ctxErr != nil {
					return "", "", ctxErr
				}
				lastErr = fmt.Errorf("init submodules for %s: %w", req.Key, err)
				continue
			}
		}
		if err := m.context().Err(); err != nil {
			return "", "", err
		}
		if err := fs.EnsureParentDir(srcDir); err != nil {
			return "", "", err
		}
		// Invalidate the identity before destroying the old tree: a crash
		// between here and the state write must force a clean re-materialization.
		_ = os.Remove(m.statePath(root))
		if err := fs.RemoveAll(srcDir); err != nil {
			return "", "", err
		}
		if err := fs.RenameRetry(tmp, srcDir); err != nil {
			return "", "", fmt.Errorf("publish tree %s: %w", srcDir, err)
		}
		return url, actual, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no git URL configured")
	}
	return "", "", fmt.Errorf("materialize %s at %s: %w", req.Key, ShortCommit(commit), lastErr)
}

func (m *SourceManager) cloneTreeAt(url, dir string, req SourceRequest, commit, localRef string) (string, error) {
	switch {
	case isCommitSHA(req.Ref) || (req.Ref == "" && req.Commit != ""):
		return m.cloneCommitTree(url, dir, commit)
	case req.Ref == "":
		return m.cloneHeadTree(url, dir)
	default:
		return m.cloneRefTree(url, dir, req, commit, localRef)
	}
}

// cloneCommitTree fetches exactly commit at depth 1 when only a commit id is
// known. Servers without allowReachableSHA1InWant cannot serve arbitrary
// commits; those fall back to a full clone, which is slow but correct.
func (m *SourceManager) cloneCommitTree(url, dir, commit string) (string, error) {
	if err := FetchShallowCommitContext(m.context(), url, dir, commit); err != nil {
		if ctxErr := m.context().Err(); ctxErr != nil {
			return "", ctxErr
		}
		fs.RemoveIfExists(dir)
		if err := cloneRepo(m.context(), url, dir, nil); err != nil {
			return "", err
		}
	}
	if err := CheckoutDetachContext(m.context(), dir, commit); err != nil {
		return "", err
	}
	return commit, nil
}

// cloneHeadTree clones the default-branch tip of url with depth 1.
func (m *SourceManager) cloneHeadTree(url, dir string) (string, error) {
	if err := CloneShallowContext(m.context(), url, dir); err != nil {
		return "", err
	}
	actual := m.treeHead(dir)
	if actual == "" {
		return "", fmt.Errorf("clone %s: cannot resolve HEAD", url)
	}
	if err := CheckoutDetachContext(m.context(), dir, actual); err != nil {
		return "", err
	}
	return actual, nil
}

// cloneRefTree fetches a tag or branch at depth 1 and verifies that a lock
// commit still resolves to the same commit.
func (m *SourceManager) cloneRefTree(url, dir string, req SourceRequest, commit, localRef string) (string, error) {
	if localRef == "" {
		// A lock-pinned request knows the commit but not the namespace;
		// detect it when the source is reachable so the tag ref lands in the
		// tree for git describe.
		if _, detected, err := resolveRemoteRefOne(m.context(), url, req.Ref); err == nil {
			localRef = detected
		}
	}
	if localRef == "" {
		return m.cloneCommitTree(url, dir, commit)
	}
	rev, err := FetchShallowRefContext(m.context(), url, dir, localRef, commit)
	if err != nil {
		return "", err
	}
	actual, err := revParseCommitContext(m.context(), dir, rev)
	if err != nil {
		return "", err
	}
	if req.Commit != "" && actual != req.Commit {
		return "", fmt.Errorf("%s resolves to %s, lock expects %s; run 'vmake lock update' to re-resolve", req.Ref, ShortCommit(actual), ShortCommit(req.Commit))
	}
	if err := CheckoutDetachContext(m.context(), dir, actual); err != nil {
		return "", err
	}
	return actual, nil
}

// restoreTree resets a working tree to commit, discarding earlier patch
// application and untracked leftovers without touching the network. This is
// the path taken when the resolver materialized a clean tree and the build
// phase asks for the same commit with patches.
func (m *SourceManager) restoreTree(req SourceRequest, srcDir, commit string) error {
	if err := CheckoutDetachContext(m.context(), srcDir, commit); err != nil {
		return fmt.Errorf("reset %s for %s: %w", ShortCommit(commit), req.Key, err)
	}
	if err := gitRunContext(m.context(), srcDir, []string{"clean", "-fdx"}, 0); err != nil {
		return fmt.Errorf("clean %s for %s: %w", ShortCommit(commit), req.Key, err)
	}
	if req.Submodules {
		if err := InitSubmodulesContext(m.context(), srcDir); err != nil {
			return fmt.Errorf("init submodules for %s: %w", req.Key, err)
		}
	}
	return nil
}

func stateURLs(state *TreeState) []string {
	if state == nil {
		return nil
	}
	return state.URLs
}

// ClearPatchState marks a tree as no longer carrying its patch set, forcing
// the next EnsureSource to recreate it. Used when patch application fails so a
// partially patched tree is never reused indefinitely.
func (m *SourceManager) ClearPatchState(root string) error {
	state, err := m.ReadState(root)
	if err != nil || state == nil || state.PatchHash == "" {
		return err
	}
	state.PatchHash = ""
	return jsonio.Save(m.statePath(root), state)
}

// HasTreeCommit reports whether the package tree for key already sits at
// commit, allowing fully offline reuse even when the source is unreachable.
func (m *SourceManager) HasTreeCommit(key, commit string) bool {
	if commit == "" {
		return false
	}
	root, err := m.treePath(key)
	if err != nil {
		return false
	}
	return m.CommitAt(root) == commit
}

// ShortCommit shortens a commit id for logs and errors.
func ShortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

// PatchSetHash summarizes a package's declared patch set. Paths are hashed in
// declaration order — patches form a series and usually do not commute, so
// application must land on the same content for a given hash. It feeds the
// remote BuildKey and the materialized tree identity (patch changes recreate
// the tree instead of stacking on stale modifications).
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

// IsLocalGitURL reports whether url points at a local filesystem repository,
// which is refreshed on every build instead of being cached once.
func IsLocalGitURL(url string) bool {
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

// UpdateSource floats a package working tree at origin/HEAD for
// `vmake pkg update`. An unchanged upstream commit keeps the existing tree
// (including local modifications); otherwise the tree is re-created in place.
func (m *SourceManager) UpdateSource(pkg *api.Package) error {
	urls := pkg.GitURLs()
	if len(urls) == 0 {
		return fmt.Errorf("package %s has no git URL", pkg.FullName())
	}
	key := PackageTreeKey(pkg.Repo, pkg.Name)
	root := m.TreeDir(key)
	req := SourceRequest{Key: key, URLs: urls, Refresh: true, Submodules: pkg.Submodules()}
	if state, err := m.ReadState(root); err == nil && state != nil {
		req.PatchHash = state.PatchHash
	}
	res, err := m.EnsureSource(req)
	if err != nil {
		return err
	}
	// A stale `out/` tree from a previous commit could still be linked into
	// build outputs; drop it so the next build repopulates cleanly.
	fs.RemoveIfExists(filepath.Join(res.Root, "out"))
	return nil
}

// PackageTreeKey is the tree key for a repo/package pair. Local SetGit
// packages use the "local" namespace.
func PackageTreeKey(repoName, pkgName string) string {
	if repoName == "" {
		return filepath.ToSlash(filepath.Join("local", pkgName))
	}
	return repoName + "/" + pkgName
}
