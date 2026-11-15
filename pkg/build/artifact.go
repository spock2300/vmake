package build

import (
	"path/filepath"

	"github.com/spock2300/vmake/pkg/api"
)

// ArtifactPlacement is the single description of where a target's output is
// staged and installed. Publish and install consume it instead of recomputing
// the same paths from PkgInfo/PkgInstallInfo.
type ArtifactPlacement struct {
	OutputPath      string
	ImportLib       string
	StageLibDir     string
	StageIncludeDir string
	InstallSubdir   string
	InstallName     string
	PostLinkOutputs []string
	Kind            api.TargetKind
	NoInstall       bool
	IsTest          bool
}

func NewPkgInfo(dirs api.PkgDirs, buildKey string) *PkgInfo {
	return &PkgInfo{PkgDirs: dirs, BuildKey: buildKey}
}

func PlacementFor(target *api.Target, pkgInfo *PkgInfo, targetOS string) ArtifactPlacement {
	kind := target.Kind()
	placement := ArtifactPlacement{
		Kind:        kind,
		NoInstall:   target.NoInstall(),
		IsTest:      target.IsTest(),
		InstallName: targetFilename(kind, target.Name(), targetOS),
	}
	placement.OutputPath = pkgInfo.OutputPath(placement.InstallName)
	placement.PostLinkOutputs = postLinkOutputPaths(target, pkgInfo.SourceDir, placement.OutputPath)
	if placement.OutputPath != "" {
		placement.ImportLib = importLibraryPath(pkgInfo.ResolvePath(placement.OutputPath), targetOS, kind)
	}
	if pkgInfo.InstallDir != "" {
		placement.StageLibDir = filepath.Join(pkgInfo.InstallDir, "lib")
		placement.StageIncludeDir = filepath.Join(pkgInfo.InstallDir, "include")
	}
	placement.InstallSubdir = target.InstallDir()
	if placement.InstallSubdir == "" {
		placement.InstallSubdir = kind.InstallDir()
	}
	return placement
}

func (s *Scheduler) placementFor(resolved *ResolvedTarget, pkgInfo *PkgInfo) ArtifactPlacement {
	return PlacementFor(resolved.Node.Target, pkgInfo, s.targetOS(resolved.Node.PkgName))
}
