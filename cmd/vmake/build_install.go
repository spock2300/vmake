package main

import (
	"github.com/spock2300/vmake/pkg/pipeline"
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

func installStorageRoots(ctx *RuntimeContext) []string {
	roots := []string{vmakeDir, getCacheDir(), getExtensionsDir(), getToolchainsDir(), getReposDir()}
	if ctx != nil && ctx.Paths != nil {
		roots = append(roots, ctx.Paths.DepsDir, ctx.Paths.CacheDir, ctx.Paths.ReposDir)
	}
	return roots
}
