package repo

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/flock"
	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/jsonio"
	"github.com/spock2300/vmake/internal/storage"
)

const objectCacheLayout = 1

// ObjectCache is a shared, content-addressed git object cache. Each upstream
// URL owns one bare repository under <cacheDir>/git that accumulates only the
// objects vmake has actually fetched, anchored by a refs/vmake/<commit> ref.
// Working trees are rebuilt from the cache with depth-1 local fetches, so a
// commit fetched once by any project or CI job is reused everywhere without
// touching the upstream again.
//
//	<cacheDir>/git/<urlhash>.git/            bare object store
//	<cacheDir>/git/<urlhash>.git/vmake-cache.json   namespace metadata
//	<cacheDir>/_locks/cache_<urlhash>.lock   per-repo lock, outside git/
//
// urlhash is storage.OwnerKey of the normalized URL. Every mutation happens
// under the per-repo lock; a missing, malformed or foreign entry is treated as
// a miss and rebuilt inside that lock rather than blocking the build.
type ObjectCache struct {
	root     string
	locksDir string
	enabled  bool
}

// NewObjectCache returns the object cache rooted at cacheDir. Setting
// VMAKE_GIT_CACHE=off disables it entirely.
func NewObjectCache(cacheDir string) *ObjectCache {
	return &ObjectCache{
		root:     filepath.Join(cacheDir, "git"),
		locksDir: filepath.Join(cacheDir, "_locks"),
		enabled:  !strings.EqualFold(os.Getenv("VMAKE_GIT_CACHE"), "off"),
	}
}

// Enabled reports whether the cache is active.
func (c *ObjectCache) Enabled() bool {
	return c != nil && c.enabled
}

// cacheMeta is the per-namespace metadata stored as vmake-cache.json. It
// records the namespace's origin URL (for collision/URL-change detection), the
// refs vmake has resolved and the commits it has fetched.
type cacheMeta struct {
	Layout     int                        `json:"layout"`
	URL        string                     `json:"url"`
	LastUsed   time.Time                  `json:"lastUsed,omitempty"`
	RefCommits map[string]string          `json:"refCommits,omitempty"`
	Commits    map[string]cacheCommitMeta `json:"commits,omitempty"`
}

type cacheCommitMeta struct {
	FetchedAt time.Time `json:"fetchedAt"`
	Anchored  bool      `json:"anchored,omitempty"`
}

func newCacheMeta(url string) *cacheMeta {
	return &cacheMeta{
		Layout:     objectCacheLayout,
		URL:        url,
		RefCommits: map[string]string{},
		Commits:    map[string]cacheCommitMeta{},
	}
}

func (m *cacheMeta) recordCommit(commit string) {
	if m.Commits == nil {
		m.Commits = map[string]cacheCommitMeta{}
	}
	m.Commits[commit] = cacheCommitMeta{FetchedAt: time.Now().UTC(), Anchored: true}
}

func (m *cacheMeta) recordRef(ref, commit string) {
	if m.RefCommits == nil {
		m.RefCommits = map[string]string{}
	}
	m.RefCommits[ref] = commit
}

// CacheEntry describes one cached upstream repository for cache listing.
type CacheEntry struct {
	URL      string
	Path     string
	LastUsed time.Time
	Commits  int
}

// RepoFileURL renders an absolute filesystem path as a file:// URL with forward
// slashes. git treats a file:// URL as a URL, so upload-pack runs and the
// --depth flag is honored exactly as for a network remote; a bare local path
// would be cloned with its full history instead.
func RepoFileURL(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	// The URL path must start with '/': on Windows "C:\x" would otherwise land
	// in the authority ("file://C:/x") instead of the path ("file:///C:/x").
	slash := "/" + strings.TrimPrefix(filepath.ToSlash(abs), "/")
	return (&url.URL{Scheme: "file", Path: slash}).String()
}

// normalizeCacheURL applies the cache's minimal URL normalization so the same
// upstream repository maps to one namespace regardless of cosmetic spelling: a
// trailing slash and a single ".git" suffix are dropped and the host is
// lowercased. Paths, ports and credentials are otherwise preserved.
func normalizeCacheURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = strings.TrimRight(s, "/")
	// Dropping one trailing ".git" merges "repo" and "repo.git" into a single
	// namespace: both spell the same upstream repository, and vmake only ever
	// clones by URL, so the cosmetic suffix must not split the object cache.
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimRight(s, "/")
	if i := strings.Index(s, "://"); i >= 0 {
		rest := s[i+3:]
		authority, path := rest, ""
		if j := strings.Index(rest, "/"); j >= 0 {
			authority, path = rest[:j], rest[j:]
		}
		if at := strings.LastIndex(authority, "@"); at >= 0 {
			authority = authority[:at+1] + lowerHostPrefix(authority[at+1:])
		} else {
			authority = lowerHostPrefix(authority)
		}
		s = s[:i+3] + authority + path
	} else if at := strings.LastIndex(s, "@"); at >= 0 {
		// scp-style "user@host:path": lowercase only the host, never the
		// userinfo before '@' or the path after ':'.
		s = s[:at+1] + lowerHostPrefix(s[at+1:])
	}
	return s
}

// lowerHostPrefix lowercases the host portion of an authority or scp-style
// "host:path" up to the first ':', leaving any port, path or credentials as
// written.
func lowerHostPrefix(s string) string {
	if i := strings.Index(s, ":"); i >= 0 {
		return strings.ToLower(s[:i]) + s[i:]
	}
	return strings.ToLower(s)
}

// refCandidates lists the cache-local ref names a bare ref may map to. The
// cache stores a branch under refs/heads/<name>; cloneRefTree maps that to
// refs/remotes/origin/<name> when it fetches the working tree.
func refCandidates(ref string) []string {
	if strings.HasPrefix(ref, "refs/") {
		return []string{ref}
	}
	return []string{"refs/tags/" + ref, "refs/heads/" + ref}
}

func (c *ObjectCache) repoPath(norm string) string {
	return filepath.Join(c.root, storage.OwnerKey(norm)+".git")
}

// RepoDir is the bare repository directory for url.
func (c *ObjectCache) RepoDir(url string) string {
	return c.repoPath(normalizeCacheURL(url))
}

func (c *ObjectCache) metaPath(repoDir string) string {
	return filepath.Join(repoDir, "vmake-cache.json")
}

func (c *ObjectCache) lockPath(norm string) string {
	return filepath.Join(c.locksDir, "cache_"+storage.OwnerKey(norm)[:16]+".lock")
}

func (c *ObjectCache) lockEntry(ctx context.Context, norm string) (*flock.FileLock, error) {
	return flock.AcquireContext(ctx, c.lockPath(norm))
}

// withLock runs fn while holding the namespace's exclusive cache lock, so a
// read (for example a working-tree checkout) cannot race a rebuild or Remove.
func (c *ObjectCache) withLock(ctx context.Context, url string, fn func() error) error {
	if !c.Enabled() {
		return fmt.Errorf("git object cache disabled")
	}
	norm := normalizeCacheURL(url)
	if norm == "" {
		return fmt.Errorf("invalid cache url %q", url)
	}
	lock, err := c.lockEntry(ctx, norm)
	if err != nil {
		return err
	}
	defer lock.Release()
	return fn()
}

// entry loads a namespace's metadata when it is usable for url. A missing,
// malformed, layout-mismatched or URL-mismatched entry reports not-ok so the
// caller rebuilds it.
func (c *ObjectCache) entry(norm, url string) (*cacheMeta, string, bool) {
	repoDir := c.repoPath(norm)
	if !dirExists(repoDir) {
		return nil, repoDir, false
	}
	var meta cacheMeta
	if err := jsonio.Load(c.metaPath(repoDir), &meta); err != nil {
		return nil, repoDir, false
	}
	if meta.Layout != objectCacheLayout || meta.URL == "" || normalizeCacheURL(meta.URL) != norm {
		return nil, repoDir, false
	}
	return &meta, repoDir, true
}

func (c *ObjectCache) saveMeta(repoDir string, meta *cacheMeta) error {
	meta.Layout = objectCacheLayout
	return jsonio.Save(c.metaPath(repoDir), meta)
}

func (c *ObjectCache) lookup(url string) (*cacheMeta, string, bool) {
	if !c.Enabled() {
		return nil, "", false
	}
	norm := normalizeCacheURL(url)
	if norm == "" {
		return nil, "", false
	}
	return c.entry(norm, url)
}

func (c *ObjectCache) refCommit(url, ref string) (commit, localRef string, ok bool) {
	if ref == "" {
		return "", "", false
	}
	meta, _, ok := c.lookup(url)
	if !ok {
		return "", "", false
	}
	for _, cand := range refCandidates(ref) {
		if commit := meta.RefCommits[cand]; commit != "" && validCommitID(commit) {
			return commit, cand, true
		}
	}
	return "", "", false
}

// Has reports whether url's commit is present in the cache and anchored. It is
// local-only and returns false on any doubt, never blocking on the network. It
// backs the cache inventory API (with List and Remove) and gates the
// commit-only fallback in cloneRefTree when a ref cannot be refreshed; the
// regular materialization path uses EnsureCommit/EnsureRef/CachedRef instead.
func (c *ObjectCache) Has(url, commit string) bool {
	if !validCommitID(commit) {
		return false
	}
	_, repoDir, ok := c.lookup(url)
	if !ok {
		return false
	}
	return c.hasAnchor(context.Background(), repoDir, commit)
}

// CachedRef returns the local ref name the cache records for ref at commit, or
// not-found when the cache has no such mapping. It lets a lock-pinned
// materialization keep the tag in the tree without a network probe.
func (c *ObjectCache) CachedRef(url, ref, wantCommit string) (string, bool) {
	if ref == "" || !validCommitID(wantCommit) {
		return "", false
	}
	meta, _, ok := c.lookup(url)
	if !ok {
		return "", false
	}
	for _, cand := range refCandidates(ref) {
		if meta.RefCommits[cand] == wantCommit {
			return cand, true
		}
	}
	return "", false
}

// CachedRefCommit returns the commit the cache last recorded for ref, if any.
// It is local-only and lets an unreachable upstream still resolve a ref that
// was materialized before (see EnsureSource's offline resolution fallback).
func (c *ObjectCache) CachedRefCommit(url, ref string) (string, string, bool) {
	return c.refCommit(url, ref)
}

// EnsureCommit guarantees that commit, with the objects reachable at depth 1,
// is present in the cache, fetching it once from url when absent. A cache hit
// touches no network at all.
func (c *ObjectCache) EnsureCommit(ctx context.Context, url, commit string) error {
	return c.ensureCommit(ctx, url, commit, false)
}

// RecordCommit is the best-effort variant of EnsureCommit used right after a
// shallow clone already produced the objects. It attempts only the depth-1
// fetch by SHA: a failed attempt reports an error instead of widening to a
// ref-tip fetch, so recording a freshly cloned HEAD can never turn into a large
// download. Callers treat the error as "not recorded yet", not as a failure.
func (c *ObjectCache) RecordCommit(ctx context.Context, url, commit string) error {
	return c.ensureCommit(ctx, url, commit, true)
}

func (c *ObjectCache) ensureCommit(ctx context.Context, url, commit string, bestEffort bool) error {
	return c.ensureCacheEntry(ctx, url, func(norm string) error {
		if norm == "" || !validCommitID(commit) {
			return fmt.Errorf("invalid cache request url=%q commit=%q", url, commit)
		}
		return nil
	}, func(ctx context.Context, repoDir string, meta *cacheMeta) error {
		return c.ensureCommitLocked(ctx, repoDir, url, meta, commit, bestEffort)
	})
}

// EnsureRef guarantees that ref (a fully qualified local ref name such as
// refs/tags/v1.0.0 or refs/heads/main) points at commit inside the cache,
// fetching the ref from url once when it is absent. Branch refs are stored under
// refs/heads/<name>; the working-tree fetch later maps them to
// refs/remotes/origin/<name>. The commit objects are anchored under
// refs/vmake/<commit> either way, so a later commit-only checkout stays
// offline-capable even when the ref has moved upstream.
func (c *ObjectCache) EnsureRef(ctx context.Context, url, ref, commit string) error {
	return c.ensureCacheEntry(ctx, url, func(norm string) error {
		if norm == "" || ref == "" || !validCommitID(commit) {
			return fmt.Errorf("invalid cache ref request url=%q ref=%q", url, ref)
		}
		return nil
	}, func(ctx context.Context, repoDir string, meta *cacheMeta) error {
		return c.ensureRefLocked(ctx, repoDir, url, meta, ref, commit)
	})
}

func (c *ObjectCache) ensureCacheEntry(
	ctx context.Context,
	url string,
	validate func(norm string) error,
	ensure func(ctx context.Context, repoDir string, meta *cacheMeta) error,
) error {
	if !c.Enabled() {
		return fmt.Errorf("git object cache disabled")
	}
	norm := normalizeCacheURL(url)
	if err := validate(norm); err != nil {
		return err
	}
	lock, err := c.lockEntry(ctx, norm)
	if err != nil {
		return err
	}
	defer lock.Release()

	repoDir, meta, created, err := c.ensureRepo(ctx, norm, url)
	if err != nil {
		return err
	}
	if err := ensure(ctx, repoDir, meta); err != nil {
		if created {
			// Do not leave an empty shell behind when the very first fetch
			// fails: a later attempt rebuilds cleanly instead of trusting it.
			fs.RemoveIfExists(repoDir)
		}
		return err
	}
	meta.LastUsed = time.Now().UTC()
	return c.saveMeta(repoDir, meta)
}

// List returns every usable cache namespace. Ordering is by URL for stable
// output.
func (c *ObjectCache) List() ([]CacheEntry, error) {
	dirs, err := os.ReadDir(c.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []CacheEntry
	for _, dir := range dirs {
		if !dir.IsDir() || !strings.HasSuffix(dir.Name(), ".git") {
			continue
		}
		repoDir := filepath.Join(c.root, dir.Name())
		var meta cacheMeta
		if err := jsonio.Load(c.metaPath(repoDir), &meta); err != nil || meta.URL == "" {
			continue
		}
		out = append(out, CacheEntry{URL: meta.URL, Path: repoDir, LastUsed: meta.LastUsed, Commits: len(meta.Commits)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URL < out[j].URL })
	return out, nil
}

// Remove deletes the cache namespace for url. It is a no-op for an unknown URL
// and leaves the namespace's lock file in place. It deliberately ignores
// Enabled(): a cache populated while the cache was on must still be removable
// after VMAKE_GIT_CACHE=off.
func (c *ObjectCache) Remove(url string) error {
	norm := normalizeCacheURL(url)
	if norm == "" {
		return nil
	}
	lock, err := c.lockEntry(context.Background(), norm)
	if err != nil {
		return err
	}
	defer lock.Release()
	return fs.RemoveAll(c.repoPath(norm))
}

// ensureRepo returns the usable bare repository for a namespace, rebuilding a
// missing, corrupt or foreign entry from scratch while holding the entry lock.
// created reports whether the repository was (re)initialized here, so a failed
// first fetch can be cleaned up without discarding a populated cache.
func (c *ObjectCache) ensureRepo(ctx context.Context, norm, url string) (string, *cacheMeta, bool, error) {
	if meta, repoDir, ok := c.entry(norm, url); ok {
		return repoDir, meta, false, nil
	}
	repoDir := c.repoPath(norm)
	if err := fs.RemoveAll(repoDir); err != nil {
		return "", nil, false, err
	}
	if err := fs.EnsureDir(repoDir); err != nil {
		return "", nil, false, err
	}
	if err := gitRunContext(ctx, repoDir, []string{"init", "--bare"}, 0); err != nil {
		return "", nil, false, fmt.Errorf("init object cache %s: %w", repoDir, err)
	}
	if err := gitRunContext(ctx, repoDir, []string{"remote", "add", "origin", url}, 0); err != nil {
		return "", nil, false, fmt.Errorf("add origin to object cache %s: %w", repoDir, err)
	}
	return repoDir, newCacheMeta(url), true, nil
}

func (c *ObjectCache) ensureCommitLocked(ctx context.Context, repoDir, url string, meta *cacheMeta, commit string, bestEffort bool) error {
	if c.hasAnchor(ctx, repoDir, commit) {
		meta.recordCommit(commit)
		return nil
	}
	if err := c.fetchCommitObjects(ctx, repoDir, url, commit, bestEffort); err != nil {
		return err
	}
	return c.anchorLocked(ctx, repoDir, meta, commit)
}

func (c *ObjectCache) ensureRefLocked(ctx context.Context, repoDir, url string, meta *cacheMeta, ref, commit string) error {
	if got, err := c.revParse(ctx, repoDir, ref); err == nil && got == commit {
		if err := c.anchorLocked(ctx, repoDir, meta, commit); err != nil {
			return err
		}
		// Record the mapping even on the local hit: without it a later
		// lock-pinned request probes ls-remote (up to VMAKE_GIT_TIMEOUT) even
		// though the ref is already cached at the requested commit.
		meta.recordRef(ref, commit)
		return nil
	}
	if err := c.fetchInto(ctx, repoDir, url, "+"+ref+":"+ref); err != nil {
		return err
	}
	got, err := c.revParse(ctx, repoDir, ref)
	if err != nil {
		return fmt.Errorf("cache ref %s missing after fetch: %w", ref, err)
	}
	if got != commit {
		return fmt.Errorf("cache ref %s resolves to %s, want %s", ref, ShortCommit(got), ShortCommit(commit))
	}
	if err := c.anchorLocked(ctx, repoDir, meta, commit); err != nil {
		return err
	}
	meta.recordRef(ref, commit)
	return nil
}

// anchorLocked pins commit under refs/vmake/<commit> so it survives gc and can
// be served to a depth-1 fetch without relying on allowAnySHA1InWant.
func (c *ObjectCache) anchorLocked(ctx context.Context, repoDir string, meta *cacheMeta, commit string) error {
	if c.hasAnchor(ctx, repoDir, commit) {
		meta.recordCommit(commit)
		return nil
	}
	if _, err := c.revParse(ctx, repoDir, commit); err != nil {
		return fmt.Errorf("cache is missing commit %s: %w", ShortCommit(commit), err)
	}
	if err := gitRunContext(ctx, repoDir, []string{"update-ref", "refs/vmake/" + commit, commit}, 0); err != nil {
		return fmt.Errorf("anchor cache commit %s: %w", ShortCommit(commit), err)
	}
	meta.recordCommit(commit)
	return nil
}

func (c *ObjectCache) hasAnchor(ctx context.Context, repoDir, commit string) bool {
	got, err := c.revParse(ctx, repoDir, "refs/vmake/"+commit)
	return err == nil && got == commit
}

func (c *ObjectCache) revParse(ctx context.Context, repoDir, rev string) (string, error) {
	out, err := runGitCmd([]string{"rev-parse", "--verify", rev + "^{commit}"}, exec.RunOptions{
		Context: ctx, Dir: repoDir, Quiet: true,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (c *ObjectCache) fetchInto(ctx context.Context, repoDir, url, spec string) error {
	args := progressArgs([]string{"fetch", "--depth", "1", url, spec})
	_, err := runGitCmd(args, networkGitOptions(ctx, repoDir))
	if err != nil {
		return networkGitError(fmt.Sprintf("git fetch %s from %s", spec, url), err)
	}
	return nil
}

func (c *ObjectCache) fetchCommitObjects(ctx context.Context, repoDir, url, commit string, bestEffort bool) error {
	if err := c.fetchInto(ctx, repoDir, url, commit); err == nil {
		return nil
	} else if ctx.Err() != nil {
		return ctx.Err()
	}
	if bestEffort {
		// Recording must never widen the fetch: the caller already cloned the
		// objects, so a refusal here just means "not cached yet".
		return fmt.Errorf("cache did not record commit %s from %s", ShortCommit(commit), url)
	}
	// A server that refuses arbitrary SHAs (allowAnySHA1InWant off) cannot
	// serve the commit by id. The widest fallback allowed is a depth-1 fetch of
	// every ref tip, which anchors the commit when it is a tip and never
	// downloads full history; older commits rely on the caller's network clone.
	return c.fetchRefTipsInto(ctx, repoDir, url)
}

// fetchRefTipsInto fetches every branch and tag tip at depth 1. It is the
// bounded fallback for servers that reject by-SHA fetches; the anchor still
// only succeeds when the commit is one of those tips.
func (c *ObjectCache) fetchRefTipsInto(ctx context.Context, repoDir, url string) error {
	args := progressArgs([]string{
		"fetch", "--depth", "1", url,
		"+refs/heads/*:refs/remotes/origin/*",
		"+refs/tags/*:refs/tags/*",
	})
	_, err := runGitCmd(args, networkGitOptions(ctx, repoDir))
	if err != nil {
		return networkGitError(fmt.Sprintf("git fetch %s", url), err)
	}
	return nil
}
