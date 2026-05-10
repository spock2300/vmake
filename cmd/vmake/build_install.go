package main

import (
	"fmt"
	"path/filepath"
	"time"

	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/internal/jsonio"
	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/build"
	"gitee.com/spock2300/vmake/pkg/config"
	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/resolver"
	"gitee.com/spock2300/vmake/pkg/version"
)

type installManifestEntry struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Source  string `json:"source"`
	URL     string `json:"url,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Path    string `json:"path,omitempty"`
}

type installManifest struct {
	VMake     string                 `json:"vmake"`
	Toolchain string                 `json:"toolchain"`
	Mode      string                 `json:"mode"`
	Generated string                 `json:"generated"`
	Packages  []installManifestEntry `json:"packages"`
}

func executeInstall(ctx *RuntimeContext, result *BuildResult) error {
	globalValues := config.BuildGlobalValues(ctx.Config)

	effectivePrefix := prefixFlag
	if effectivePrefix == "" {
		effectivePrefix = filepath.Join(ctx.WorkDir, "install")
	}

	vlog.Info("")
	vlog.Info("Installing...")

	fs.RemoveIfExists(effectivePrefix)
	fs.EnsureDir(effectivePrefix)

	installer := build.NewArtifactInstaller(result.Graph, result.PkgDirs, effectivePrefix)
	installer.SetInstallType(installTypeFlag)

	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]
		if node.Pkg == nil {
			continue
		}
		installOnePackage(ctx, name, node, result, installer, globalValues)
	}

	if err := installer.InstallAll(); err != nil {
		return err
	}

	if err := writeManifest(ctx, result, effectivePrefix); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	vlog.Info("")
	vlog.Info("Install succeeded!")
	return nil
}

func installOnePackage(ctx *RuntimeContext, name string, node *resolver.PackageNode, result *BuildResult, installer *build.ArtifactInstaller, globalValues map[string]any) {
	entry := config.GetEntry(ctx.Config, name)

	installCtx := api.NewInstallContext(name, entry.Options)
	installCtx.SetOptions(ctx.AllOptions[name])
	installCtx.MergeGlobals(ctx.GlobalOptions, globalValues)

	node.Pkg.ExecInstallFuncs(result.PkgDirs[name].SourceDir, func(fn api.InstallFunc) {
		fn(installCtx)
	})

	buildCtx := newBuildContext(ctx, name, globalValues)
	buildCtx.SetDryRun(true)
	buildCtx.SetBuildSubGraphFunc(func(string) error { return nil })
	buildCtx.SetDepOutputFunc(func(string) string { return "" })
	node.Pkg.SetDryRun(true)
	node.Pkg.ExecBuildFuncs(result.PkgDirs[name].SourceDir, func(fn api.BuildFunc) {
		fn(buildCtx)
	})
	node.Pkg.SetDryRun(false)

	installItems := installCtx.GetInstallItems()
	installItems = append(installItems, buildCtx.GetInstallItems()...)

	var installFilter api.InstallFilterFunc
	if installCtx.GetInstallFilter() != nil {
		installFilter = installCtx.GetInstallFilter()
	} else if buildCtx.GetInstallFilter() != nil {
		installFilter = buildCtx.GetInstallFilter()
	}

	installer.SetPackageInfo(name, &build.PkgInstallInfo{
		Targets:       result.AllTargets[name],
		InstallItems:  installItems,
		BuildDir:      result.PkgDirs[name].BuildDir,
		Mode:          result.Mode,
		TcName:        result.TcName,
		BuildKey:      result.PkgBuildKeys[name],
		InstallFilter: installFilter,
	})
}

func writeManifest(ctx *RuntimeContext, result *BuildResult, effectivePrefix string) error {
	var packages []installManifestEntry
	for _, name := range ctx.DepGraph.Order {
		node := ctx.DepGraph.Packages[name]
		if node.IsLocal() {
			sourceDir := result.PkgDirs[name].SourceDir
			relPath, _ := filepath.Rel(ctx.WorkDir, sourceDir)
			gitDir := node.Pkg.SrcDir()
			packages = append(packages, installManifestEntry{
				Name:    name,
				Version: gitDescribe(gitDir),
				Source:  "local",
				Ref:     gitRevParse(gitDir),
				Path:    relPath,
			})
			continue
		}
		ip, ok := result.InstalledPkgs[name]
		if !ok {
			continue
		}
		entry := installManifestEntry{
			Name:    name,
			Version: ip.Version,
			Source:  "registry",
		}
		if node.IsNative() {
			entry.Source = "native"
			entry.URL = node.Native.GitURL
			if ref, ok := node.Native.Versions[ip.Version]; ok {
				entry.Ref = ref
			}
		} else if node.Pkg != nil {
			urls := node.Pkg.GitURLs()
			if len(urls) > 0 {
				entry.URL = urls[0]
			}
			if ref, ok := node.Pkg.Versions()[ip.Version]; ok {
				entry.Ref = ref
			}
		}
		packages = append(packages, entry)
	}
	mf := installManifest{
		VMake:     version.Version,
		Toolchain: result.TcName,
		Mode:      result.Mode,
		Generated: time.Now().UTC().Format(time.RFC3339),
		Packages:  packages,
	}
	path := filepath.Join(effectivePrefix, "manifest.json")
	return jsonio.Save(path, mf)
}
