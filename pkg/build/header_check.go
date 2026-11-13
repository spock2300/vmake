package build

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/glob"
	"github.com/spock2300/vmake/pkg/api"
)

type dirListing struct {
	exact  map[string]bool
	folded map[string]string
}

type pathCanonCache struct {
	mu    sync.Mutex
	dirs  map[string]*dirListing
	canon map[string]string
}

func newPathCanonCache() *pathCanonCache {
	return &pathCanonCache{
		dirs:  make(map[string]*dirListing),
		canon: make(map[string]string),
	}
}

func (c *pathCanonCache) canonPath(abs string) string {
	if c == nil || abs == "" {
		return abs
	}
	c.mu.Lock()
	if hit, ok := c.canon[abs]; ok {
		c.mu.Unlock()
		return hit
	}
	c.mu.Unlock()

	parent := filepath.Dir(abs)
	if parent == abs {
		return abs
	}
	canonParent := c.canonPath(parent)
	base := filepath.Base(abs)

	listing := c.readDir(canonParent)
	if listing != nil && !listing.exact[base] {
		if actual, ok := listing.folded[strings.ToLower(base)]; ok && actual != base {
			p1 := filepath.Join(canonParent, base)
			p2 := filepath.Join(canonParent, actual)
			if i1, err1 := os.Stat(p1); err1 == nil {
				if i2, err2 := os.Stat(p2); err2 == nil && os.SameFile(i1, i2) {
					base = actual
				}
			}
		}
	}

	res := filepath.Join(canonParent, base)
	c.mu.Lock()
	c.canon[abs] = res
	c.mu.Unlock()
	return res
}

func (c *pathCanonCache) readDir(dir string) *dirListing {
	c.mu.Lock()
	if hit, ok := c.dirs[dir]; ok {
		c.mu.Unlock()
		return hit
	}
	c.mu.Unlock()

	entries, err := os.ReadDir(dir)
	var listing *dirListing
	if err == nil {
		listing = &dirListing{
			exact:  make(map[string]bool, len(entries)),
			folded: make(map[string]string, len(entries)),
		}
		for _, e := range entries {
			name := e.Name()
			listing.exact[name] = true
			low := strings.ToLower(name)
			if _, exists := listing.folded[low]; !exists {
				listing.folded[low] = name
			}
		}
	}

	c.mu.Lock()
	c.dirs[dir] = listing
	c.mu.Unlock()
	return listing
}

type allowedInclude struct {
	path     string
	realPath string
	rules    []string
	isFile   bool
}

func newAllowedInclude(rawPath string, rules []string, canon *pathCanonCache) (allowedInclude, bool) {
	if strings.TrimSpace(rawPath) == "" {
		return allowedInclude{}, false
	}
	abs := cleanAbs(rawPath)
	if abs == "" {
		return allowedInclude{}, false
	}
	if canon != nil {
		abs = canon.canonPath(abs)
	}
	real := evalCleanNearestAncestor(abs)
	if canon != nil && real != "" {
		real = canon.canonPath(real)
	}
	isFile := false
	if info, err := os.Stat(abs); err == nil && !info.IsDir() {
		isFile = true
	}
	return allowedInclude{
		path:     abs,
		realPath: real,
		rules:    rules,
		isFile:   isFile,
	}, true
}

func (a allowedInclude) matches(absHeader string) bool {
	if absHeader == "" || a.path == "" {
		return false
	}
	matched := false
	if a.isFile {
		matched = absHeader == a.path || (a.realPath != "" && absHeader == a.realPath)
	} else {
		matched = hasPathPrefix(absHeader, a.path) || (a.realPath != "" && hasPathPrefix(absHeader, a.realPath))
	}
	if !matched {
		return false
	}
	if len(a.rules) > 0 {
		return MatchPatterns(a.rules, filepath.Base(absHeader))
	}
	return true
}

type pkgDirEntry struct {
	dir     string
	pkgName string
}

type headerOwnerIndex struct {
	entries []pkgDirEntry
}

func (s *Scheduler) getHeaderOwners() *headerOwnerIndex {
	s.headerOwnersOnce.Do(func() {
		s.headerOwners = s.buildHeaderOwnerIndex()
	})
	return s.headerOwners
}

func (s *Scheduler) buildHeaderOwnerIndex() *headerOwnerIndex {
	type dirPkg struct {
		dir     string
		pkgName string
	}
	seen := make(map[dirPkg]bool)
	hasPkgAtDir := make(map[string]bool)
	addDir := func(pkgName, raw string) {
		if pkgName == "" || strings.TrimSpace(raw) == "" {
			return
		}
		abs := cleanAbs(raw)
		if abs == "" {
			return
		}
		if s.canonCache != nil {
			abs = s.canonCache.canonPath(abs)
		}
		seen[dirPkg{dir: abs, pkgName: pkgName}] = true
		hasPkgAtDir[abs] = true
		if real := evalCleanNearestAncestor(abs); real != "" {
			if s.canonCache != nil {
				real = s.canonCache.canonPath(real)
			}
			if real != abs {
				seen[dirPkg{dir: real, pkgName: pkgName}] = true
				hasPkgAtDir[real] = true
			}
		}
	}

	pkgNames := make([]string, 0, len(s.pkgs)+len(s.packages)+len(s.workspaceDirs))
	pkgNameSet := make(map[string]bool)
	for name := range s.workspaceDirs {
		if !pkgNameSet[name] {
			pkgNameSet[name] = true
			pkgNames = append(pkgNames, name)
		}
	}
	for name := range s.pkgs {
		if !pkgNameSet[name] {
			pkgNameSet[name] = true
			pkgNames = append(pkgNames, name)
		}
	}
	for name := range s.packages {
		if !pkgNameSet[name] {
			pkgNameSet[name] = true
			pkgNames = append(pkgNames, name)
		}
	}
	sort.Strings(pkgNames)

	for _, name := range pkgNames {
		if wsDir := s.workspaceDirs[name]; wsDir != "" {
			addDir(name, wsDir)
		}
		if info := s.pkgs[name]; info != nil {
			addDir(name, info.SourceDir)
			if info.BuildDir != "" || info.BuildKey != "" {
				addDir(name, resolveWorkPath(info.SourceDir, info.EffectiveBuildDir()))
			}
			if info.InstallDir != "" {
				addDir(name, resolveWorkPath(info.SourceDir, info.InstallDir))
			}
		}
		if pkg := s.packages[name]; pkg != nil {
			addDir(name, pkg.SourceDir())
			addDir(name, pkg.SrcDir())
			addDir(name, pkg.BuildDir())
			addDir(name, pkg.InstallDir())
		}
	}

	addManagedBoundary := func(raw, ownerName string) {
		if strings.TrimSpace(raw) == "" {
			return
		}
		abs := cleanAbs(raw)
		if abs == "" {
			return
		}
		if s.canonCache != nil {
			abs = s.canonCache.canonPath(abs)
		}
		if !hasPkgAtDir[abs] {
			seen[dirPkg{dir: abs, pkgName: ownerName}] = true
		}
		if real := evalCleanNearestAncestor(abs); real != "" {
			if s.canonCache != nil {
				real = s.canonCache.canonPath(real)
			}
			if !hasPkgAtDir[real] {
				seen[dirPkg{dir: real, pkgName: ownerName}] = true
			}
		}
	}
	if s.rootDir != "" {
		for _, sub := range []string{".vmake_deps", ".vmake", "install", "build"} {
			addManagedBoundary(filepath.Join(s.rootDir, sub), "")
		}
	}
	for _, wsDir := range s.workspaceDirs {
		if wsDir == "" {
			continue
		}
		for _, sub := range []string{".vmake_deps", ".vmake", "install"} {
			addManagedBoundary(filepath.Join(wsDir, sub), "")
		}
	}

	entries := make([]pkgDirEntry, 0, len(seen))
	for dp := range seen {
		entries = append(entries, pkgDirEntry{dir: dp.dir, pkgName: dp.pkgName})
	}
	sort.Slice(entries, func(i, j int) bool {
		if len(entries[i].dir) != len(entries[j].dir) {
			return len(entries[i].dir) > len(entries[j].dir)
		}
		if entries[i].dir != entries[j].dir {
			return entries[i].dir < entries[j].dir
		}
		return entries[i].pkgName < entries[j].pkgName
	})
	return &headerOwnerIndex{entries: entries}
}

func (idx *headerOwnerIndex) ownerOfFor(absPath, currentPkg string) string {
	if idx == nil || absPath == "" {
		return ""
	}
	matchedDir := ""
	firstOwner := ""
	for _, e := range idx.entries {
		if matchedDir != "" && e.dir != matchedDir {
			break
		}
		if hasPathPrefix(absPath, e.dir) {
			matchedDir = e.dir
			if e.pkgName == currentPkg {
				return currentPkg
			}
			if firstOwner == "" {
				firstOwner = e.pkgName
			}
		}
	}
	return firstOwner
}

func (idx *headerOwnerIndex) ownerOf(absPath string) string {
	return idx.ownerOfFor(absPath, "")
}

func isGroupingUmbrellaTarget(t *api.Target) bool {
	if t == nil || t.Kind() != api.TargetVoid {
		return false
	}
	return t.BuildFunc() == nil &&
		len(t.Outputs()) == 0 &&
		len(t.ProvidedLibs()) == 0 &&
		len(t.PublicIncludes()) == 0
}

func (s *Scheduler) isFirstPartyPkg(pkgName string) bool {
	if s.graph != nil && s.graph.PkgMeta != nil {
		if meta, ok := s.graph.PkgMeta[pkgName]; ok {
			return meta.IsFirstParty()
		}
	}
	return true
}

func (s *Scheduler) populateHeaderBoundaries(node *BuildNode, resolved *ResolvedTarget) {
	resolved.DeclaredDepPkgs = make(map[string]bool)
	resolved.DirectDepPkgs = make(map[string]bool)
	if s.graph == nil || node == nil {
		return
	}

	appendAllowed := func(raw string, rules []string) {
		if inc, ok := newAllowedInclude(raw, rules, s.canonCache); ok {
			resolved.AllowedPublicIncludes = append(resolved.AllowedPublicIncludes, inc)
		}
	}
	appendDirectAllowed := func(raw string, rules []string) {
		if inc, ok := newAllowedInclude(raw, rules, s.canonCache); ok {
			resolved.DirectAllowedPublicIncludes = append(resolved.DirectAllowedPublicIncludes, inc)
		}
	}

	resolveDepNodeInclude := func(depPkgName, pubInc string) string {
		if depPkg := s.pkgs[depPkgName]; depPkg != nil {
			if strings.HasPrefix(pubInc, api.BuildDirToken) && depPkg.BuildDir == "" && depPkg.BuildKey == "" {
				return ""
			}
			return depPkg.ResolvePath(pubInc)
		}
		if srcDir := s.pkgSourceDir(depPkgName); srcDir != "" && !strings.HasPrefix(pubInc, api.BuildDirToken) {
			return resolvePkgPath(srcDir, "", pubInc)
		}
		return ""
	}

	for _, depPkgName := range node.DeclaredDepPkgs {
		if depPkgName != "" && depPkgName != node.PkgName {
			resolved.DeclaredDepPkgs[depPkgName] = true
			if depPkg := s.pkgs[depPkgName]; depPkg != nil && depPkg.InstallDir != "" {
				appendAllowed(filepath.Join(depPkg.InstallDir, "include"), nil)
			}
		}
	}

	collectNodeIncludes := func(depNode *BuildNode, emit func(string, []string)) {
		if depNode == nil || depNode.Target == nil {
			return
		}
		for _, pubInc := range depNode.Target.PublicIncludes() {
			if strings.TrimSpace(pubInc) == "" {
				continue
			}
			rules := depNode.Target.IncludeRule(pubInc)
			resolvedInc := resolveDepNodeInclude(depNode.PkgName, pubInc)
			if resolvedInc != "" {
				emit(resolvedInc, rules)
			}
			if pkg := s.packages[depNode.PkgName]; pkg != nil && pkg.SrcDirRaw() != "" {
				if !filepath.IsAbs(pubInc) && !strings.HasPrefix(pubInc, api.BuildDirToken) {
					srcAlt := filepath.Join(pkg.SrcDir(), pubInc)
					if cleanAbs(srcAlt) != cleanAbs(resolvedInc) {
						emit(srcAlt, rules)
					}
				}
			}
		}
		if depPkg := s.pkgs[depNode.PkgName]; depPkg != nil && depPkg.InstallDir != "" {
			emit(filepath.Join(depPkg.InstallDir, "include"), nil)
		}
	}

	visitedNodes := map[string]bool{node.FullName: true}
	queue := append([]string{}, node.Deps...)

	for i := 0; i < len(queue); i++ {
		depName := queue[i]
		if visitedNodes[depName] {
			continue
		}
		visitedNodes[depName] = true
		depNode := s.graph.Nodes[depName]
		if depNode == nil {
			continue
		}
		queue = append(queue, depNode.Deps...)
		if depNode.PkgName != node.PkgName {
			resolved.DeclaredDepPkgs[depNode.PkgName] = true
		}
		collectNodeIncludes(depNode, appendAllowed)
	}

	visitedDirect := map[string]bool{node.FullName: true}
	directQueue := append([]string{}, node.DirectDeps...)
	for i := 0; i < len(directQueue); i++ {
		depName := directQueue[i]
		if visitedDirect[depName] {
			continue
		}
		visitedDirect[depName] = true
		depNode := s.graph.Nodes[depName]
		if depNode == nil {
			continue
		}
		if depNode.PkgName != node.PkgName {
			resolved.DirectDepPkgs[depNode.PkgName] = true
		}
		collectNodeIncludes(depNode, appendDirectAllowed)
		if depNode.PkgName == node.PkgName || isGroupingUmbrellaTarget(depNode.Target) {
			directQueue = append(directQueue, depNode.DirectDeps...)
		}
	}

	if meta, ok := s.graph.PkgMeta[node.PkgName]; ok && len(meta.SubGraphs) > 0 {
		subPkgs := s.collectReachableSubGraphPkgs(meta.SubGraphs)
		for subPkg := range subPkgs {
			if subPkg != node.PkgName {
				resolved.DeclaredDepPkgs[subPkg] = true
				resolved.DirectDepPkgs[subPkg] = true
				if depPkg := s.pkgs[subPkg]; depPkg != nil && depPkg.InstallDir != "" {
					incDir := filepath.Join(depPkg.InstallDir, "include")
					appendAllowed(incDir, nil)
					appendDirectAllowed(incDir, nil)
				}
			}
		}
		for _, cand := range s.graph.Nodes {
			if subPkgs[cand.PkgName] {
				if !visitedNodes[cand.FullName] {
					visitedNodes[cand.FullName] = true
					collectNodeIncludes(cand, appendAllowed)
				}
				if !visitedDirect[cand.FullName] {
					visitedDirect[cand.FullName] = true
					collectNodeIncludes(cand, appendDirectAllowed)
				}
			}
		}
	}
}

func (s *Scheduler) collectReachableSubGraphPkgs(roots []string) map[string]bool {
	visited := make(map[string]bool)
	var visit func(pkg string)
	visit = func(pkg string) {
		if pkg == "" || visited[pkg] {
			return
		}
		visited[pkg] = true
		if meta, ok := s.graph.PkgMeta[pkg]; ok {
			for _, d := range meta.Deps {
				visit(d)
			}
			for _, sub := range meta.SubGraphs {
				visit(sub)
			}
		}
		for _, n := range s.graph.Nodes {
			if n.PkgName != pkg {
				continue
			}
			for _, depFull := range n.Deps {
				if depNode := s.graph.Nodes[depFull]; depNode != nil {
					visit(depNode.PkgName)
				}
			}
		}
	}
	for _, r := range roots {
		visit(r)
	}
	return visited
}

func (r *ResolvedTarget) allowsHeader(absHeader string) bool {
	if absHeader == "" {
		return false
	}
	for _, inc := range r.AllowedPublicIncludes {
		if inc.matches(absHeader) {
			return true
		}
	}
	return false
}

func (r *ResolvedTarget) allowsDirectHeader(absHeader string) bool {
	if absHeader == "" {
		return false
	}
	for _, inc := range r.DirectAllowedPublicIncludes {
		if inc.matches(absHeader) {
			return true
		}
	}
	return false
}

func (r *ResolvedTarget) allowsIncludeDir(absDir string) bool {
	if absDir == "" {
		return false
	}
	for _, inc := range r.AllowedPublicIncludes {
		if inc.isFile || len(inc.rules) > 0 {
			continue
		}
		if hasPathPrefix(absDir, inc.path) || (inc.realPath != "" && hasPathPrefix(absDir, inc.realPath)) {
			return true
		}
	}
	return false
}

func (s *Scheduler) crossPkgOwner(owners *headerOwnerIndex, currentPkg, absPath string) string {
	if absPath == "" {
		return ""
	}
	if s.canonCache != nil {
		absPath = s.canonCache.canonPath(absPath)
	}
	owner := owners.ownerOfFor(absPath, currentPkg)
	if owner != "" && owner != currentPkg && !s.isTargetlessAncestor(owner, currentPkg) {
		return owner
	}
	if real := evalCleanNearestAncestor(absPath); real != "" {
		if s.canonCache != nil {
			real = s.canonCache.canonPath(real)
		}
		if real != absPath {
			realOwner := owners.ownerOfFor(real, currentPkg)
			if realOwner != "" && realOwner != currentPkg && !s.isTargetlessAncestor(realOwner, currentPkg) {
				return realOwner
			}
		}
	}
	return ""
}

func stripGlobBase(pattern string) string {
	idx := strings.IndexAny(pattern, "*?[")
	if idx < 0 {
		return pattern
	}
	prefix := pattern[:idx]
	if slash := strings.LastIndexAny(prefix, `/\`); slash >= 0 {
		if slash == 0 {
			return prefix[:1]
		}
		return prefix[:slash]
	}
	return "."
}

func isParentRelative(raw string) bool {
	if filepath.IsAbs(raw) {
		return false
	}
	slash := filepath.ToSlash(filepath.Clean(raw))
	return slash == ".." || strings.HasPrefix(slash, "../")
}

func (s *Scheduler) checkTargetDeclaredPaths(resolved *ResolvedTarget, sourceDir, buildDir string) error {
	if resolved == nil || resolved.Node == nil || resolved.Node.Target == nil || sourceDir == "" {
		return nil
	}
	node := resolved.Node
	if !s.isFirstPartyPkg(node.PkgName) {
		return nil
	}
	owners := s.getHeaderOwners()
	t := node.Target

	for _, raw := range t.Files() {
		if strings.TrimSpace(raw) == "" || strings.HasPrefix(raw, api.BuildDirToken) {
			continue
		}
		base := stripGlobBase(raw)
		abs := cleanAbs(resolvePkgPath(sourceDir, buildDir, base))
		if owner := s.crossPkgOwner(owners, node.PkgName, abs); owner != "" {
			return fmt.Errorf(
				"cross-package source path: target %s AddFiles(%q) points into package %q",
				node.FullName, raw, owner,
			)
		}
	}

	if prebuilt := t.Prebuilt(); strings.TrimSpace(prebuilt) != "" && !strings.HasPrefix(prebuilt, api.BuildDirToken) {
		abs := cleanAbs(resolvePkgPath(sourceDir, buildDir, prebuilt))
		if owner := s.crossPkgOwner(owners, node.PkgName, abs); owner != "" {
			return fmt.Errorf(
				"cross-package prebuilt path: target %s SetPrebuilt(%q) points into package %q",
				node.FullName, prebuilt, owner,
			)
		}
	}

	for _, raw := range t.PublicIncludes() {
		if strings.TrimSpace(raw) == "" || strings.HasPrefix(raw, api.BuildDirToken) {
			continue
		}
		abs := cleanAbs(resolvePkgPath(sourceDir, buildDir, raw))
		if owner := s.crossPkgOwner(owners, node.PkgName, abs); owner != "" {
			return fmt.Errorf(
				"cross-package public include: target %s AddPublicIncludes(%q) points into package %q",
				node.FullName, raw, owner,
			)
		}
	}

	for _, raw := range t.Includes() {
		if strings.TrimSpace(raw) == "" || strings.HasPrefix(raw, api.BuildDirToken) {
			continue
		}
		abs := cleanAbs(resolvePkgPath(sourceDir, buildDir, raw))
		if s.canonCache != nil && abs != "" {
			abs = s.canonCache.canonPath(abs)
		}
		owner := s.crossPkgOwner(owners, node.PkgName, abs)
		if owner == "" {
			continue
		}
		if !resolved.DeclaredDepPkgs[owner] {
			return fmt.Errorf(
				"undeclared header dependency: target %s includes directory %q owned by package %q, which is not a declared dependency (add AddDeps(%q))",
				node.FullName, raw, owner, owner,
			)
		}
		real := evalCleanNearestAncestor(abs)
		if s.canonCache != nil && real != "" {
			real = s.canonCache.canonPath(real)
		}
		if !resolved.allowsIncludeDir(abs) && (real == "" || !resolved.allowsIncludeDir(real)) {
			return fmt.Errorf(
				"private header inclusion: target %s includes private directory %q of package %q (not exposed via AddPublicIncludes)",
				node.FullName, raw, owner,
			)
		}
		if isParentRelative(raw) {
			return fmt.Errorf(
				"cross-package relative include: target %s AddIncludes(%q) escapes into package %q (rely on AddDeps + AddPublicIncludes instead of ../)",
				node.FullName, raw, owner,
			)
		}
	}

	for _, raw := range t.Inputs() {
		if strings.TrimSpace(raw) == "" || strings.HasPrefix(raw, api.BuildDirToken) {
			continue
		}
		if !isParentRelative(raw) {
			continue
		}
		abs := cleanAbs(resolvePkgPath(sourceDir, buildDir, raw))
		if owner := s.crossPkgOwner(owners, node.PkgName, abs); owner != "" {
			return fmt.Errorf(
				"cross-package relative input: target %s AddInputs(%q) escapes into package %q (use DepOutput, DepSrcDir, or DepPackageDir instead of ../)",
				node.FullName, raw, owner,
			)
		}
	}

	return nil
}

type directInclude struct {
	spec    string
	quoted  bool
	guarded bool
}

var standardCHeaders = map[string]bool{
	"assert.h": true, "complex.h": true, "ctype.h": true, "dirent.h": true,
	"dlfcn.h": true, "endian.h": true, "errno.h": true, "fcntl.h": true,
	"fenv.h": true, "float.h": true, "fnmatch.h": true, "getopt.h": true,
	"glob.h": true, "grp.h": true, "iconv.h": true, "inttypes.h": true,
	"iso646.h": true, "langinfo.h": true, "libgen.h": true, "limits.h": true,
	"locale.h": true, "malloc.h": true, "math.h": true, "memory.h": true,
	"mqueue.h": true, "netdb.h": true, "poll.h": true, "pthread.h": true,
	"pwd.h": true, "regex.h": true, "sched.h": true, "search.h": true,
	"semaphore.h": true, "setjmp.h": true, "signal.h": true, "spawn.h": true,
	"stdalign.h": true, "stdarg.h": true, "stdatomic.h": true, "stdbool.h": true,
	"stddef.h": true, "stdint.h": true, "stdio.h": true, "stdlib.h": true,
	"stdnoreturn.h": true, "string.h": true, "strings.h": true, "syslog.h": true,
	"tar.h": true, "termios.h": true, "tgmath.h": true, "threads.h": true,
	"time.h": true, "uchar.h": true, "unistd.h": true, "utime.h": true,
	"wchar.h": true, "wctype.h": true,
}

func isStandardCHeader(spec string) bool {
	s := filepath.ToSlash(strings.TrimSpace(spec))
	if standardCHeaders[s] {
		return true
	}
	for _, prefix := range []string{"sys/", "arpa/", "netinet/", "net/", "machine/", "bits/", "asm/"} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func parseDirectIncludes(absSrc string) []directInclude {
	data, err := os.ReadFile(absSrc)
	if err != nil || len(data) == 0 {
		return nil
	}
	var out []directInclude
	inBlockComment := false
	ifDepth := 0

	var stripped strings.Builder
	lines := strings.Split(string(data), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSuffix(lines[i], "\r")
		for strings.HasSuffix(line, `\`) && i+1 < len(lines) {
			i++
			line = line[:len(line)-1] + strings.TrimSuffix(lines[i], "\r")
		}
		stripped.Reset()
		inStr := byte(0)
		for j := 0; j < len(line); j++ {
			ch := line[j]
			if inBlockComment {
				if ch == '*' && j+1 < len(line) && line[j+1] == '/' {
					inBlockComment = false
					j++
					stripped.WriteByte(' ')
				}
				continue
			}
			if inStr != 0 {
				stripped.WriteByte(ch)
				if ch == '\\' && j+1 < len(line) {
					j++
					stripped.WriteByte(line[j])
				} else if ch == inStr {
					inStr = 0
				}
				continue
			}
			if ch == '/' && j+1 < len(line) {
				if line[j+1] == '/' {
					break
				}
				if line[j+1] == '*' {
					inBlockComment = true
					j++
					stripped.WriteByte(' ')
					continue
				}
			}
			if ch == '"' || ch == '\'' {
				inStr = ch
			}
			stripped.WriteByte(ch)
		}
		s := strings.TrimSpace(stripped.String())
		if !strings.HasPrefix(s, "#") {
			continue
		}
		rest := strings.TrimSpace(s[1:])
		if rest == "" {
			continue
		}
		endKw := 0
		for endKw < len(rest) && ((rest[endKw] >= 'a' && rest[endKw] <= 'z') || (rest[endKw] >= 'A' && rest[endKw] <= 'Z') || rest[endKw] == '_') {
			endKw++
		}
		kw := rest[:endKw]
		arg := strings.TrimSpace(rest[endKw:])
		switch kw {
		case "if", "ifdef", "ifndef":
			ifDepth++
		case "endif":
			if ifDepth > 0 {
				ifDepth--
			}
		case "include":
			if len(arg) < 3 {
				continue
			}
			if arg[0] == '"' {
				if end := strings.IndexByte(arg[1:], '"'); end > 0 {
					out = append(out, directInclude{
						spec:    arg[1 : 1+end],
						quoted:  true,
						guarded: ifDepth > 0,
					})
				}
			} else if arg[0] == '<' {
				if end := strings.IndexByte(arg[1:], '>'); end > 0 {
					out = append(out, directInclude{
						spec:    arg[1 : 1+end],
						quoted:  false,
						guarded: ifDepth > 0,
					})
				}
			}
		}
	}
	return out
}

func (s *Scheduler) isUpstreamGitSource(pkgName, absSrc string) bool {
	pkg := s.packages[pkgName]
	if pkg == nil || pkg.SrcDirRaw() == "" {
		return false
	}
	upstream := cleanAbs(pkg.SrcDir())
	srcDir := cleanAbs(s.pkgSourceDir(pkgName))
	if upstream == "" || upstream == srcDir {
		return false
	}
	if s.canonCache != nil {
		upstream = s.canonCache.canonPath(upstream)
	}
	return hasPathPrefix(absSrc, upstream)
}

func (s *Scheduler) checkHeaderBoundaries(resolved *ResolvedTarget, src string, deps []string) error {
	if len(deps) == 0 || resolved == nil || resolved.Node == nil {
		return nil
	}
	pkgInfo := s.pkgs[resolved.Node.PkgName]
	if pkgInfo == nil {
		return nil
	}
	workDir := pkgInfo.SourceDir
	absSrc := cleanAbs(resolveWorkPath(workDir, src))
	if s.canonCache != nil && absSrc != "" {
		absSrc = s.canonCache.canonPath(absSrc)
	}
	owners := s.getHeaderOwners()

	type resolvedDepHeader struct {
		absH  string
		realH string
		slash string
		owner string
	}
	resolvedDeps := make([]resolvedDepHeader, 0, len(deps))

	for _, dep := range deps {
		if strings.TrimSpace(dep) == "" {
			continue
		}
		absH := cleanAbs(resolveWorkPath(workDir, dep))
		if absH == "" {
			continue
		}
		if s.canonCache != nil {
			absH = s.canonCache.canonPath(absH)
		}
		if absH == absSrc {
			continue
		}
		realH := evalCleanNearestAncestor(absH)
		if s.canonCache != nil && realH != "" {
			realH = s.canonCache.canonPath(realH)
		}
		owner := s.crossPkgOwner(owners, resolved.Node.PkgName, absH)
		resolvedDeps = append(resolvedDeps, resolvedDepHeader{
			absH:  absH,
			realH: realH,
			slash: filepath.ToSlash(absH),
			owner: owner,
		})

		if resolved.allowsHeader(absH) || (realH != "" && realH != absH && resolved.allowsHeader(realH)) {
			continue
		}
		if owner == "" {
			continue
		}

		displayH := s.displayHeaderPath(absH, dep)
		if !resolved.DeclaredDepPkgs[owner] {
			return fmt.Errorf(
				"undeclared header dependency: target %s (%s) includes %q owned by package %q, which is not a declared dependency (add AddDeps(%q))",
				resolved.Node.FullName, src, displayH, owner, owner,
			)
		}
		return fmt.Errorf(
			"private header inclusion: target %s (%s) includes private header %q of package %q (not exposed via AddPublicIncludes)",
			resolved.Node.FullName, src, displayH, owner,
		)
	}

	if !s.isFirstPartyPkg(resolved.Node.PkgName) || s.isUpstreamGitSource(resolved.Node.PkgName, absSrc) {
		return nil
	}

	for _, inc := range parseDirectIncludes(absSrc) {
		if inc.guarded {
			continue
		}
		if !inc.quoted && isStandardCHeader(inc.spec) {
			continue
		}
		cleanSpec := filepath.ToSlash(filepath.Clean(inc.spec))
		if cleanSpec == "" || cleanSpec == "." {
			continue
		}
		var candLocal string
		if inc.quoted && absSrc != "" {
			candLocal = cleanAbs(filepath.Join(filepath.Dir(absSrc), inc.spec))
			if s.canonCache != nil && candLocal != "" {
				candLocal = s.canonCache.canonPath(candLocal)
			}
		}
		suffix := "/" + cleanSpec
		var matched []resolvedDepHeader
		for _, dh := range resolvedDeps {
			if (candLocal != "" && dh.absH == candLocal) || strings.HasSuffix(dh.slash, suffix) || dh.slash == cleanSpec {
				matched = append(matched, dh)
			}
		}
		if len(matched) == 0 {
			continue
		}
		allowedDirect := false
		firstCrossOwner := ""
		for _, dh := range matched {
			if dh.owner == "" {
				allowedDirect = true
				break
			}
			if resolved.allowsDirectHeader(dh.absH) || (dh.realH != "" && dh.realH != dh.absH && resolved.allowsDirectHeader(dh.realH)) {
				allowedDirect = true
				break
			}
			if firstCrossOwner == "" {
				firstCrossOwner = dh.owner
			}
		}
		if !allowedDirect && firstCrossOwner != "" {
			return fmt.Errorf(
				"transitive header inclusion: target %s (%s) directly includes %q of package %q, which is only transitively reachable (add direct AddDeps(%q))",
				resolved.Node.FullName, src, inc.spec, firstCrossOwner, firstCrossOwner,
			)
		}
	}

	return nil
}

func (s *Scheduler) checkStaticSourceIncludes(resolved *ResolvedTarget, src, sourceDir string) error {
	if resolved == nil || resolved.Node == nil || sourceDir == "" {
		return nil
	}
	node := resolved.Node
	if !s.isFirstPartyPkg(node.PkgName) {
		return nil
	}
	absSrc := cleanAbs(resolveWorkPath(sourceDir, src))
	if s.canonCache != nil && absSrc != "" {
		absSrc = s.canonCache.canonPath(absSrc)
	}
	if absSrc == "" || s.isUpstreamGitSource(node.PkgName, absSrc) {
		return nil
	}
	owners := s.getHeaderOwners()
	pkg := s.packages[node.PkgName]
	hasGenerated := len(node.Target.GenRules()) > 0 || (pkg != nil && (pkg.GenConfigHeader() || pkg.HasGenerators()))

	var ownDirs []string
	for _, raw := range append(append([]string{}, node.Target.Includes()...), node.Target.PublicIncludes()...) {
		if strings.TrimSpace(raw) == "" || strings.HasPrefix(raw, api.BuildDirToken) {
			continue
		}
		ownDirs = append(ownDirs, cleanAbs(resolvePkgPath(sourceDir, "", raw)))
	}

	matchAllowedSpec := func(list []allowedInclude, spec string) string {
		for _, a := range list {
			if a.isFile {
				if filepath.Base(a.path) == spec && fs.FileExists(a.path) {
					return a.path
				}
				continue
			}
			cand := filepath.Join(a.path, filepath.FromSlash(spec))
			if fs.FileExists(cand) && a.matches(cleanAbs(cand)) {
				return cleanAbs(cand)
			}
		}
		return ""
	}

	for _, inc := range parseDirectIncludes(absSrc) {
		if inc.guarded {
			continue
		}
		if !inc.quoted && isStandardCHeader(inc.spec) {
			continue
		}
		if inc.quoted {
			relCand := cleanAbs(filepath.Join(filepath.Dir(absSrc), inc.spec))
			if fs.FileExists(relCand) {
				if s.canonCache != nil {
					relCand = s.canonCache.canonPath(relCand)
				}
				owner := s.crossPkgOwner(owners, node.PkgName, relCand)
				if owner == "" {
					continue
				}
				real := evalCleanNearestAncestor(relCand)
				if s.canonCache != nil && real != "" {
					real = s.canonCache.canonPath(real)
				}
				if !resolved.allowsHeader(relCand) && (real == "" || !resolved.allowsHeader(real)) {
					displayH := s.displayHeaderPath(relCand, inc.spec)
					if !resolved.DeclaredDepPkgs[owner] {
						return fmt.Errorf(
							"undeclared header dependency: target %s (%s) includes %q owned by package %q, which is not a declared dependency (add AddDeps(%q))",
							node.FullName, src, displayH, owner, owner,
						)
					}
					return fmt.Errorf(
						"private header inclusion: target %s (%s) includes private header %q of package %q (not exposed via AddPublicIncludes)",
						node.FullName, src, displayH, owner,
					)
				}
				if !resolved.allowsDirectHeader(relCand) && (real == "" || !resolved.allowsDirectHeader(real)) {
					return fmt.Errorf(
						"transitive header inclusion: target %s (%s) directly includes %q of package %q, which is only transitively reachable (add direct AddDeps(%q))",
						node.FullName, src, inc.spec, owner, owner,
					)
				}
				continue
			}
		}

		foundOwn := false
		for _, d := range ownDirs {
			if d != "" && fs.FileExists(filepath.Join(d, filepath.FromSlash(inc.spec))) {
				foundOwn = true
				break
			}
		}
		if foundOwn || hasGenerated {
			continue
		}
		if matchAllowedSpec(resolved.DirectAllowedPublicIncludes, inc.spec) != "" {
			continue
		}
		if transHit := matchAllowedSpec(resolved.AllowedPublicIncludes, inc.spec); transHit != "" {
			if owner := s.crossPkgOwner(owners, node.PkgName, transHit); owner != "" {
				return fmt.Errorf(
					"transitive header inclusion: target %s (%s) directly includes %q of package %q, which is only transitively reachable (add direct AddDeps(%q))",
					node.FullName, src, inc.spec, owner, owner,
				)
			}
		}
	}
	return nil
}

func (s *Scheduler) pkgSourceDir(pkgName string) string {
	if info := s.pkgs[pkgName]; info != nil && info.SourceDir != "" {
		return info.SourceDir
	}
	if dir := s.workspaceDirs[pkgName]; dir != "" {
		return dir
	}
	if pkg := s.packages[pkgName]; pkg != nil && pkg.SourceDir() != "" {
		return pkg.SourceDir()
	}
	return ""
}

func (s *Scheduler) isTargetlessAncestor(ownerPkg, childPkg string) bool {
	if ownerPkg == "" || childPkg == "" || ownerPkg == childPkg {
		return false
	}
	inMeta := false
	if s.graph != nil {
		for _, n := range s.graph.Nodes {
			if n.PkgName == ownerPkg {
				return false
			}
		}
		if s.graph.PkgMeta != nil {
			_, inMeta = s.graph.PkgMeta[ownerPkg]
		}
	}
	if !inMeta {
		if pkg := s.packages[ownerPkg]; pkg != nil && pkg.HasBuildFuncs() {
			return false
		}
	}
	ownerDir := cleanAbs(s.pkgSourceDir(ownerPkg))
	childDir := cleanAbs(s.pkgSourceDir(childPkg))
	if ownerDir == "" || childDir == "" {
		return false
	}
	if s.canonCache != nil {
		ownerDir = s.canonCache.canonPath(ownerDir)
		childDir = s.canonCache.canonPath(childDir)
	}
	return hasPathPrefix(childDir, ownerDir)
}

func (s *Scheduler) displayHeaderPath(absPath, fallback string) string {
	if s.rootDir != "" {
		rootAbs := cleanAbs(s.rootDir)
		if s.canonCache != nil {
			rootAbs = s.canonCache.canonPath(rootAbs)
		}
		if rel, err := filepath.Rel(rootAbs, absPath); err == nil {
			slash := filepath.ToSlash(rel)
			if slash != ".." && !strings.HasPrefix(slash, "../") {
				return slash
			}
		}
	}
	return filepath.ToSlash(fallback)
}

func cleanAbs(p string) string {
	if strings.TrimSpace(p) == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return filepath.Clean(abs)
}

func evalCleanNearestAncestor(abs string) string {
	if abs == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(real)
	}
	var tail []string
	cur := abs
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			out := filepath.Clean(real)
			for i := len(tail) - 1; i >= 0; i-- {
				out = filepath.Join(out, tail[i])
			}
			return out
		}
	}
}

func hasPathPrefix(path, prefix string) bool {
	if path == "" || prefix == "" {
		return false
	}
	if path == prefix {
		return true
	}
	if strings.HasSuffix(prefix, string(filepath.Separator)) {
		return strings.HasPrefix(path, prefix)
	}
	return strings.HasPrefix(path, prefix+string(filepath.Separator))
}

type BoundaryViolation struct {
	Kind    string
	Package string
	Target  string
	Message string
}

func CheckExistingBuildBoundaries(
	graph *BuildGraph,
	layouts map[string]*PackageLayout,
	packages map[string]*api.Package,
	workspaceDirs map[string]string,
	rootDir string,
	mode string,
) []BoundaryViolation {
	if graph == nil {
		return nil
	}
	if mode == "" {
		mode = api.ModeDebug
	}
	if packages == nil {
		packages = make(map[string]*api.Package)
	}
	s := &Scheduler{
		graph:         graph,
		mode:          mode,
		pkgs:          make(map[string]*PkgInfo, len(layouts)+len(workspaceDirs)),
		packages:      packages,
		workspaceDirs: workspaceDirs,
		rootDir:       rootDir,
		canonCache:    newPathCanonCache(),
	}
	for name, layout := range layouts {
		if layout == nil || layout.Dirs.SourceDir == "" {
			continue
		}
		buildKey := layout.BuildKey
		dirs := layout.Dirs
		if !layout.Ready {
			buildKey = ""
			dirs.BuildDir = ""
		}
		s.pkgs[name] = &PkgInfo{
			PkgDirs:  dirs,
			BuildKey: buildKey,
		}
	}
	for name, dir := range workspaceDirs {
		if dir != "" && s.pkgs[name] == nil {
			s.pkgs[name] = &PkgInfo{
				PkgDirs: api.PkgDirs{SourceDir: dir},
			}
		}
	}
	for name, pkg := range packages {
		if pkg != nil && pkg.SourceDir() != "" && s.pkgs[name] == nil {
			s.pkgs[name] = &PkgInfo{
				PkgDirs: api.PkgDirs{SourceDir: pkg.SourceDir()},
			}
		}
	}

	var violations []BoundaryViolation
	seenMsg := make(map[string]bool)
	record := func(kind string, node *BuildNode, err error) {
		if err == nil {
			return
		}
		msg := err.Error()
		if seenMsg[msg] {
			return
		}
		seenMsg[msg] = true
		violations = append(violations, BoundaryViolation{
			Kind:    kind,
			Package: node.PkgName,
			Target:  node.FullName,
			Message: msg,
		})
	}

	for _, fullName := range graph.Order {
		node := graph.Nodes[fullName]
		if node == nil || node.Target == nil {
			continue
		}
		pkgInfo := s.pkgs[node.PkgName]
		if pkgInfo == nil || pkgInfo.SourceDir == "" {
			continue
		}
		hasBuildKey := pkgInfo.BuildDir != "" || pkgInfo.BuildKey != ""
		effBuildDir := ""
		buildDirExists := false
		if hasBuildKey {
			effBuildDir = pkgInfo.EffectiveBuildDir()
			buildDir := resolveWorkPath(pkgInfo.SourceDir, effBuildDir)
			if info, err := os.Stat(buildDir); err == nil && info.IsDir() {
				buildDirExists = true
			}
		}

		resolved := &ResolvedTarget{
			Node: node,
		}
		s.populateHeaderBoundaries(node, resolved)

		if perr := s.checkTargetDeclaredPaths(resolved, pkgInfo.SourceDir, effBuildDir); perr != nil {
			record("path-boundary", node, perr)
			continue
		}

		excludes := node.Target.ExcludedFiles()
		for _, rawPattern := range node.Target.Files() {
			if strings.HasPrefix(rawPattern, api.BuildDirToken) && !hasBuildKey {
				continue
			}
			pattern := rawPattern
			if hasBuildKey {
				pattern = expandBuildDirToken(effBuildDir, rawPattern)
			}
			files, err := glob.Match(pattern, pkgInfo.SourceDir)
			if err != nil {
				continue
			}
			for _, f := range files {
				if !matchesAny(f, excludes) {
					resolved.SourceFiles = append(resolved.SourceFiles, f)
				}
			}
		}
		resolved.SourceFiles = unique(resolved.SourceFiles)

		for _, src := range resolved.SourceFiles {
			checkedDepFile := false
			if buildDirExists {
				if objSub, err := objectPath(node.FullName, src, pkgInfo.SourceDir); err == nil {
					objRel := pkgInfo.OutputPath(objSub)
					absDep := resolveWorkPath(pkgInfo.SourceDir, objRel+".d")
					if deps, err := ParseDepFile(absDep); err == nil {
						checkedDepFile = true
						if herr := s.checkHeaderBoundaries(resolved, src, deps); herr != nil {
							record("header-boundary", node, herr)
						}
					}
				}
			}
			if !checkedDepFile {
				if herr := s.checkStaticSourceIncludes(resolved, src, pkgInfo.SourceDir); herr != nil {
					record("header-boundary", node, herr)
				}
			}
		}
	}

	return violations
}
