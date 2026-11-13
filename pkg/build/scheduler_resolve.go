package build

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/glob"
	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
)

func resolveProvidedLib(installDir, lib string) (artifact, ldflag string) {
	if installDir == "" {
		return "", "-l" + lib
	}
	archivePath := filepath.Join(fs.DetectLibDir(installDir), "lib"+lib+".a")
	if fs.FileExists(archivePath) {
		return archivePath, ""
	}
	return "", "-l" + lib
}

func (s *Scheduler) collectDepArtifacts(node *BuildNode) (*depResolveResult, error) {
	result := &depResolveResult{}
	visited := map[string]bool{node.FullName: true}
	var transQueue []string

	collectPublicAttrs := func(depNode *BuildNode, depPkg *PkgInfo) {
		if len(depNode.Target.PublicIncludes()) > 0 {
			for _, pubInc := range depNode.Target.PublicIncludes() {
				result.includes = append(result.includes, depPkg.ResolvePath(pubInc))
			}
		} else if depPkg.InstallDir != "" {
			result.includes = append(result.includes, filepath.Join(depPkg.InstallDir, "include"))
		}
		result.defines = append(result.defines, depNode.Target.PublicDefines()...)
		result.cflags = append(result.cflags, depNode.Target.PublicCFlags()...)
		result.cxxflags = append(result.cxxflags, depNode.Target.PublicCxxFlags()...)
		result.ldflags = append(result.ldflags, depNode.Target.PublicLdFlags()...)
		result.links = append(result.links, depNode.Target.PublicLinks()...)
	}

	for _, depName := range node.Deps {
		visited[depName] = true
		depNode, err := s.graph.GetNode(depName)
		if err != nil {
			return nil, fmt.Errorf("dependency not found: %s", depName)
		}
		transQueue = append(transQueue, depNode.Deps...)

		depPkg := s.pkgs[depNode.PkgName]
		if depPkg == nil {
			continue
		}

		if err := s.ensurePackageGenerated(depNode.PkgName); err != nil {
			return nil, err
		}

		collectPublicAttrs(depNode, depPkg)

		if depNode.Target.Kind() == api.TargetVoid && depPkg.InstallDir != "" {
			if len(depNode.Target.ProvidedLibs()) > 0 {
				for _, lib := range depNode.Target.ProvidedLibs() {
					artifact, ldflag := resolveProvidedLib(depPkg.InstallDir, lib)
					if artifact != "" {
						result.artifacts = append(result.artifacts, artifact)
					} else {
						result.voidLdFlags = append(result.voidLdFlags, ldflag)
					}
				}
			} else if artifact, _ := resolveProvidedLib(depPkg.InstallDir, depNode.Target.Name()); artifact != "" {
				result.artifacts = append(result.artifacts, artifact)
			}
		} else if depNode.Target.Kind() != api.TargetVoid {
			var depOutput string
			if depPkg.InstallDir != "" && depPkg.BuildDir == "" {
				depOutput = filepath.Join(depPkg.InstallDir, "lib", targetFilename(depNode.Target.Kind(), depNode.Target.Name(), s.targetOS(depNode.PkgName)))
			} else {
				depOutput = s.getTargetOutputPath(depNode)
			}
			// A PE shared library is linked against its import library, which
			// LinkShared emits next to the DLL as libfoo.dll.a. Prebuilt
			// targets ship only the declared file, so consumers link the DLL
			// itself.
			if s.targetOS(depNode.PkgName) == "windows" && depNode.Target.Kind() == api.TargetShared && depNode.Target.Prebuilt() == "" {
				depOutput += ".a"
			}
			if !fs.FileExists(depOutput) {
				return nil, fmt.Errorf("dependency artifact missing: %s (was the dependency target %s built?)", depOutput, depName)
			}
			if depNode.Target.IsLinkable() {
				result.artifacts = append(result.artifacts, depOutput)

				for _, lib := range depNode.Target.ProvidedLibs() {
					if lib == depNode.Target.Name() {
						continue
					}
					artifact, ldflag := resolveProvidedLib(depPkg.InstallDir, lib)
					if artifact != "" {
						result.artifacts = append(result.artifacts, artifact)
					} else {
						result.voidLdFlags = append(result.voidLdFlags, ldflag)
					}
				}
			}
		}
	}

	for i := 0; i < len(transQueue); i++ {
		transName := transQueue[i]
		if visited[transName] {
			continue
		}
		visited[transName] = true
		transNode, err := s.graph.GetNode(transName)
		if err != nil {
			return nil, fmt.Errorf("dependency not found: %s", transName)
		}
		transQueue = append(transQueue, transNode.Deps...)
		transPkg := s.pkgs[transNode.PkgName]
		if transPkg == nil {
			continue
		}
		if err := s.ensurePackageGenerated(transNode.PkgName); err != nil {
			return nil, err
		}
		collectPublicAttrs(transNode, transPkg)
	}

	return result, nil
}

func (s *Scheduler) resolveTarget(node *BuildNode) (*ResolvedTarget, error) {
	pkgInfo, err := s.pkgInfoOf(node.PkgName)
	if err != nil {
		return nil, err
	}
	buildDir := pkgInfo.EffectiveBuildDir()

	modeFlags, modeDefines := api.GetModeFlags(s.mode)
	var visibilityCFlags, visibilityCxxFlags []string
	if pkg := s.packages[node.PkgName]; pkg != nil {
		visibilityCFlags, visibilityCxxFlags = pkg.VisibilityFlags()
	}

	resolved := &ResolvedTarget{
		Node:        node,
		AllDefines:  append([]string{}, node.Target.Defines()...),
		AllCFlags:   append(visibilityCFlags, node.Target.CFlags()...),
		AllCxxFlags: append(visibilityCxxFlags, node.Target.CxxFlags()...),
		AllLdFlags:  append([]string{}, node.Target.LdFlags()...),
		AllLinks:    append([]string{}, node.Target.Links()...),
	}

	resolved.AllCFlags = append(resolved.AllCFlags, modeFlags...)
	resolved.AllCFlags = append(resolved.AllCFlags, s.globalCFlags...)
	resolved.AllCxxFlags = append(resolved.AllCxxFlags, modeFlags...)
	resolved.AllCxxFlags = append(resolved.AllCxxFlags, s.globalCxxFlags...)
	resolved.AllDefines = append(resolved.AllDefines, modeDefines...)
	resolved.AllLdFlags = append(resolved.AllLdFlags, s.globalLdFlags...)
	resolved.AllLinks = append(resolved.AllLinks, s.globalLinks...)

	for _, inc := range node.Target.Includes() {
		resolved.AllIncludes = append(resolved.AllIncludes, expandBuildDirToken(buildDir, inc))
	}

	for _, pubInc := range node.Target.PublicIncludes() {
		resolved.AllIncludes = append(resolved.AllIncludes, expandBuildDirToken(buildDir, pubInc))
	}

	deps, err := s.collectDepArtifacts(node)
	if err != nil {
		return nil, err
	}
	resolved.AllIncludes = append(resolved.AllIncludes, deps.includes...)
	resolved.AllDefines = append(resolved.AllDefines, node.Target.PublicDefines()...)
	resolved.AllDefines = append(resolved.AllDefines, deps.defines...)
	resolved.AllCFlags = append(resolved.AllCFlags, node.Target.PublicCFlags()...)
	resolved.AllCFlags = append(resolved.AllCFlags, deps.cflags...)
	resolved.AllCxxFlags = append(resolved.AllCxxFlags, node.Target.PublicCxxFlags()...)
	resolved.AllCxxFlags = append(resolved.AllCxxFlags, deps.cxxflags...)
	resolved.AllLdFlags = append(resolved.AllLdFlags, node.Target.PublicLdFlags()...)
	resolved.AllLdFlags = append(resolved.AllLdFlags, deps.ldflags...)
	resolved.AllLinks = append(resolved.AllLinks, node.Target.PublicLinks()...)
	resolved.AllLinks = append(resolved.AllLinks, deps.links...)
	resolved.DepArtifacts = deps.artifacts
	s.populateHeaderBoundaries(node, resolved)
	if err := s.checkTargetDeclaredPaths(resolved, pkgInfo.SourceDir, buildDir); err != nil {
		return nil, err
	}

	s.checkLibDependencies(resolved)

	resolved.AllLdFlags = append(resolved.AllLdFlags, deps.voidLdFlags...)

	excludes := node.Target.ExcludedFiles()
	sourceDir := pkgInfo.SourceDir
	pkg := s.packages[node.PkgName]
	for _, rawPattern := range node.Target.Files() {
		pattern := expandBuildDirToken(buildDir, rawPattern)
		files, err := glob.Match(pattern, sourceDir)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if !matchesAny(f, excludes) {
				resolved.SourceFiles = append(resolved.SourceFiles, f)
			}
		}
	}

	resolved.OutputPath = s.getTargetOutputPath(node)

	genRules := node.Target.GenRules()
	needGenerated := len(genRules) > 0 || (pkg != nil && pkg.GenConfigHeader())
	if needGenerated {
		generatedDir := pkgInfo.GeneratedDir()
		resolved.AllIncludes = append(resolved.AllIncludes, generatedDir)
	}

	if vs := node.Target.VersionScript(); vs != "" {
		kind := node.Target.Kind()
		if kind != api.TargetShared && kind != api.TargetBinary {
			return nil, fmt.Errorf("SetVersionScript(%q): only valid on TargetShared/TargetBinary, got %q", vs, kind)
		}
		resolved.VersionScript = pkgInfo.ResolvePath(vs)
	}

	if node.Target.Kind() == api.TargetBinary {
		linkerScript := node.Target.LinkerScript()
		if linkerScript != "" {
			linkerScript = pkgInfo.ResolvePath(linkerScript)
		}
		if linkerScript == "" && node.Target.UseDepLinkerScript() {
			var err error
			linkerScript, err = s.resolveDependencyLinkerScript(node)
			if err != nil {
				return nil, err
			}
		}
		if node.Target.UseDepLinkerScript() && linkerScript == "" {
			return nil, fmt.Errorf("%s: no dependency provides a linker script", node.FullName)
		}
		if linkerScript != "" {
			info, err := os.Stat(linkerScript)
			if err != nil {
				return nil, fmt.Errorf("linker script %s: %w", linkerScript, err)
			} else if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("linker script %s is not a regular file", linkerScript)
			}
		}
		resolved.LinkerScript = linkerScript
	}

	if el := node.Target.ExcludeLibs(); len(el) > 0 {
		resolved.ExcludeLibs = append([]string{}, el...)
	}
	resolved.SymbolBinding = node.Target.SymbolBinding()

	resolved.AllIncludes = unique(resolved.AllIncludes)
	resolved.SourceFiles = unique(resolved.SourceFiles)

	return resolved, nil
}

func (s *Scheduler) resolveDependencyLinkerScript(node *BuildNode) (string, error) {
	resolvePkgScript := func(pkgName string) (string, bool, error) {
		depPkg := s.packages[pkgName]
		if depPkg == nil || depPkg.ProvidedLinkerScript() == "" {
			return "", false, nil
		}
		if err := s.ensurePackageGenerated(pkgName); err != nil {
			return "", false, err
		}
		if depInfo := s.pkgs[pkgName]; depInfo != nil {
			return depInfo.ResolvePath(depPkg.ProvidedLinkerScript()), true, nil
		}
		return depPkg.ProvidedLinkerScript(), true, nil
	}

	if path, ok, err := resolvePkgScript(node.PkgName); err != nil {
		return "", err
	} else if ok {
		return path, nil
	}

	type levelState struct {
		targets []string
		pkgs    []string
	}
	seenTargets := map[string]bool{node.FullName: true}
	seenPkgs := map[string]bool{node.PkgName: true}

	addFromNode := func(n *BuildNode, next *levelState) {
		hasStructured := len(n.DirectDeps) > 0 || (n.Target != nil && (n.Target.UsesRequires() || len(n.Target.Deps()) > 0))
		directTargets := n.DirectDeps
		if !hasStructured {
			directTargets = n.Deps
		}
		for _, tName := range directTargets {
			if tName == "" {
				continue
			}
			if !seenTargets[tName] {
				seenTargets[tName] = true
				next.targets = append(next.targets, tName)
			}
			pkgName, _, _ := strings.Cut(tName, ":")
			if pkgName != "" && !seenPkgs[pkgName] {
				seenPkgs[pkgName] = true
				next.pkgs = append(next.pkgs, pkgName)
			}
		}
		if n.Target != nil && s.graph != nil {
			if n.Target.UsesRequires() {
				for _, depPkg := range s.graph.PkgMeta[n.PkgName].Deps {
					if depPkg != "" && !seenPkgs[depPkg] {
						seenPkgs[depPkg] = true
						next.pkgs = append(next.pkgs, depPkg)
					}
				}
			}
			for _, rawDep := range n.Target.Deps() {
				depPkg, _, hasColon := strings.Cut(rawDep, ":")
				if !hasColon {
					depPkg = rawDep
				}
				if depPkg != "" && depPkg != n.PkgName {
					if _, ok := s.graph.PkgMeta[depPkg]; ok && !seenPkgs[depPkg] {
						seenPkgs[depPkg] = true
						next.pkgs = append(next.pkgs, depPkg)
					}
				}
			}
		}
	}

	var cur levelState
	addFromNode(node, &cur)

	for depth := 1; len(cur.targets) > 0 || len(cur.pkgs) > 0; depth++ {
		byPath := make(map[string][]string)
		var pathOrder []string
		for _, pkgName := range cur.pkgs {
			path, ok, err := resolvePkgScript(pkgName)
			if err != nil {
				return "", err
			}
			if !ok {
				continue
			}
			if _, exists := byPath[path]; !exists {
				pathOrder = append(pathOrder, path)
			}
			byPath[path] = append(byPath[path], pkgName)
		}
		if len(pathOrder) > 1 {
			var providers []string
			for _, p := range pathOrder {
				for _, pkgName := range byPath[p] {
					providers = append(providers, fmt.Sprintf("%s (%s)", pkgName, p))
				}
			}
			sort.Strings(providers)
			return "", fmt.Errorf("%s: ambiguous ProvidedLinkerScript at dependency depth %d: %s", node.FullName, depth, strings.Join(providers, ", "))
		}
		if len(pathOrder) == 1 {
			return pathOrder[0], nil
		}

		var next levelState
		for _, tName := range cur.targets {
			if depNode, err := s.graph.GetNode(tName); err == nil {
				addFromNode(depNode, &next)
			}
		}
		for _, pkgName := range cur.pkgs {
			if s.graph != nil {
				for _, depPkg := range s.graph.PkgMeta[pkgName].Deps {
					if depPkg != "" && !seenPkgs[depPkg] {
						seenPkgs[depPkg] = true
						next.pkgs = append(next.pkgs, depPkg)
					}
				}
			}
		}
		cur = next
	}

	return "", nil
}

func targetFilename(kind api.TargetKind, name, targetOS string) string {
	return api.TargetFilename(kind, name, targetOS)
}

func (s *Scheduler) targetOS(pkgName string) string {
	if pkg := s.packages[pkgName]; pkg != nil {
		return pkg.TargetOS()
	}
	return s.platform.OSOrHost()
}

func objectPath(target, src, workDir string) (string, error) {
	root, err := filepath.Abs(workDir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, resolveWorkPath(root, src))
	if err != nil {
		return "", fmt.Errorf("object source path %s: %w", src, err)
	}
	hash := sha256.Sum256([]byte(target + "\x00" + filepath.ToSlash(rel)))
	name := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel)) + ".o"
	return filepath.Join(subdirObjects, hex.EncodeToString(hash[:]), name), nil
}

func (s *Scheduler) checkLibDependencies(resolved *ResolvedTarget) {
	kind := resolved.Node.Target.Kind()
	if kind != api.TargetStatic && kind != api.TargetObject {
		return
	}
	for _, artifact := range resolved.DepArtifacts {
		if isLibraryArtifact(artifact) {
			vlog.Verbose("  NOTE %s: library dependency %s provides build ordering/includes only; it is not merged into the %s output", resolved.Node.FullName, filepath.Base(artifact), kind)
		}
	}
}

func (s *Scheduler) getTargetOutputPath(node *BuildNode) string {
	return s.pkgs[node.PkgName].OutputPath(targetFilename(node.Target.Kind(), node.Target.Name(), s.targetOS(node.PkgName)))
}

func matchesAny(path string, patterns []string) bool {
	for _, p := range patterns {
		if glob.MatchPath(p, path) {
			return true
		}
	}
	return false
}

func (s *Scheduler) generateConfigHeader(pkg *api.Package, generatedDir string) error {
	var importPkgs []*api.Package
	for _, depName := range pkg.ImportConfigs() {
		if depPkg := s.packages[depName]; depPkg != nil {
			importPkgs = append(importPkgs, depPkg)
		}
	}
	mergedOpts, mergedVals := api.MergeImportedOptions(pkg.Options, pkg.CfgVals, importPkgs)
	content := api.ConfigToHeader(mergedOpts, mergedVals)
	return api.WriteConfigHeader(generatedDir, content)
}
