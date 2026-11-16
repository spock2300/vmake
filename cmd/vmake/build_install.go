package main

import (
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/resolver"
)

const installOwnershipMarker = pipeline.InstallOwnershipMarker

type (
	installManifestEntry = pipeline.InstallManifestEntry
	installManifest      = pipeline.InstallManifest
)

func executeInstall(ctx *RuntimeContext, result *BuildResult) error {
	return pipeline.RunInstall(ctx, result, pipeline.InstallOptions{
		Prefix:       prefixFlag,
		InstallType:  installTypeFlag,
		StorageRoots: installStorageRoots(ctx),
	})
}

func installOnePackage(ctx *RuntimeContext, name string, node *resolver.PackageNode, result *BuildResult, installer *build.ArtifactInstaller, globalValues map[string]any, options map[string]*api.Option) error {
	return pipeline.InstallOnePackage(ctx, name, node, result, installer, globalValues, options, prefixFlag)
}

func validateInstallPrefix(prefix, projectDir string, storageRoots ...string) error {
	return pipeline.ValidateInstallPrefix(prefix, projectDir, storageRoots...)
}

func installStorageRoots(ctx *RuntimeContext) []string {
	roots := []string{vmakeDir, getCacheDir(), getExtensionsDir(), getToolchainsDir(), getReposDir()}
	if ctx != nil && ctx.Paths != nil {
		roots = append(roots, ctx.Paths.DepsDir, ctx.Paths.CacheDir, ctx.Paths.ReposDir)
	}
	return roots
}

func writeManifest(ctx *RuntimeContext, result *BuildResult, effectivePrefix string) error {
	return pipeline.WriteManifest(ctx, result, effectivePrefix)
}
