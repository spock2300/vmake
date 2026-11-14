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

func (s *Scheduler) placementFor(resolved *ResolvedTarget, pkgInfo *PkgInfo) ArtifactPlacement {
	target := resolved.Node.Target
	kind := target.Kind()
	targetOS := s.targetOS(resolved.Node.PkgName)
	placement := ArtifactPlacement{
		OutputPath:      resolved.OutputPath,
		InstallName:     targetFilename(kind, target.Name(), targetOS),
		PostLinkOutputs: postLinkOutputPaths(target, pkgInfo.SourceDir, resolved.OutputPath),
		Kind:            kind,
		NoInstall:       target.NoInstall(),
		IsTest:          target.IsTest(),
	}
	if resolved.OutputPath != "" {
		placement.ImportLib = importLibraryPath(pkgInfo.ResolvePath(resolved.OutputPath), targetOS, kind)
	}
	if pkgInfo.InstallDir != "" {
		placement.StageLibDir = filepath.Join(pkgInfo.InstallDir, "lib")
		placement.StageIncludeDir = filepath.Join(pkgInfo.InstallDir, "include")
	}
	if dir := target.InstallDir(); dir != "" {
		placement.InstallSubdir = dir
	} else {
		placement.InstallSubdir = kind.InstallDir()
	}
	return placement
}
