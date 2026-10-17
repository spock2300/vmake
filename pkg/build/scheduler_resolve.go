package build

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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

	for _, depName := range node.Deps {
		depNode, err := s.graph.GetNode(depName)
		if err != nil {
			return nil, fmt.Errorf("dependency not found: %s", depName)
		}

		depPkg := s.pkgs[depNode.PkgName]
		if depPkg == nil {
			continue
		}

		if len(depNode.Target.PublicIncludes()) > 0 {
			srcDir := s.pkgs[depNode.PkgName].SourceDir
			for _, pubInc := range depNode.Target.PublicIncludes() {
				result.includes = append(result.includes, filepath.Join(srcDir, pubInc))
			}
		} else if depPkg.InstallDir != "" {
			result.includes = append(result.includes, filepath.Join(depPkg.InstallDir, "include"))
		}

		if depNode.Target.Kind() == api.TargetVoid && depPkg.InstallDir != "" {
			var libNames []string
			if len(depNode.Target.ProvidedLibs()) > 0 {
				libNames = depNode.Target.ProvidedLibs()
			} else {
				libNames = []string{depNode.Target.Name()}
			}
			for _, lib := range libNames {
				artifact, ldflag := resolveProvidedLib(depPkg.InstallDir, lib)
				if artifact != "" {
					result.artifacts = append(result.artifacts, artifact)
				} else {
					result.voidLdFlags = append(result.voidLdFlags, ldflag)
				}
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

	return result, nil
}

func (s *Scheduler) resolveTarget(node *BuildNode) (*ResolvedTarget, error) {
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
		resolved.AllIncludes = append(resolved.AllIncludes, inc)
	}

	for _, pubInc := range node.Target.PublicIncludes() {
		resolved.AllIncludes = append(resolved.AllIncludes, pubInc)
	}

	deps, err := s.collectDepArtifacts(node)
	if err != nil {
		return nil, err
	}
	resolved.AllIncludes = append(resolved.AllIncludes, deps.includes...)
	resolved.DepArtifacts = deps.artifacts

	s.checkLibDependencies(resolved)

	resolved.AllLdFlags = append(resolved.AllLdFlags, deps.voidLdFlags...)

	excludes := node.Target.ExcludedFiles()
	pkgInfo, err := s.pkgInfoOf(node.PkgName)
	if err != nil {
		return nil, err
	}
	sourceDir := pkgInfo.SourceDir
	for _, pattern := range node.Target.Files() {
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
	pkg := s.packages[node.PkgName]
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
		if pkgInfo != nil {
			resolved.VersionScript = filepath.Join(pkgInfo.SourceDir, vs)
		} else {
			resolved.VersionScript = vs
		}
	}

	if node.Target.Kind() == api.TargetBinary {
		linkerScript := node.Target.LinkerScript()
		if linkerScript != "" {
			linkerScript = resolveWorkPath(pkgInfo.SourceDir, linkerScript)
		}
		if linkerScript == "" && node.Target.UseDepLinkerScript() {
			for _, depFullName := range node.Deps {
				depPkgName, _, _ := strings.Cut(depFullName, ":")
				depPkg := s.packages[depPkgName]
				if depPkg != nil && depPkg.ProvidedLinkerScript() != "" {
					if depInfo := s.pkgs[depPkgName]; depInfo != nil {
						linkerScript = filepath.Join(depInfo.SourceDir, depPkg.ProvidedLinkerScript())
					}
					break
				}
			}
		}
		if node.Target.UseDepLinkerScript() && linkerScript == "" {
			return nil, fmt.Errorf("%s: no dependency provides a linker script", node.FullName)
		}
		if linkerScript != "" {
			info, err := os.Stat(linkerScript)
			if err != nil {
				return nil, fmt.Errorf("linker script %s: %w", linkerScript, err)
			}
			if !info.Mode().IsRegular() {
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
			vlog.Info("  NOTE %s: library dependency %s provides build ordering/includes only; it is not merged into the %s output", resolved.Node.FullName, filepath.Base(artifact), kind)
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
