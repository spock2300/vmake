package build

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
)

func (s *Scheduler) publishTarget(resolved *ResolvedTarget, pkgInfo *PkgInfo) error {
	t := resolved.Node.Target
	placement := s.placementFor(resolved, pkgInfo)
	kind := placement.Kind

	if kind == api.TargetVoid || kind == api.TargetObject || placement.IsTest {
		return nil
	}

	libDir := placement.StageLibDir
	includeDir := placement.StageIncludeDir
	published := false

	if placement.OutputPath != "" {
		srcPath := resolveWorkPath(pkgInfo.SourceDir, placement.OutputPath)
		dest := filepath.Join(libDir, filepath.Base(placement.OutputPath))
		if info, err := os.Stat(dest); err == nil && info.Mode().IsRegular() &&
			sameFileContent(srcPath, dest) && !implibMissingAt(placement.ImportLib, libDir) {
			vlog.Info("  SKIP (already published)")
			published = true
		}
	}

	if err := os.MkdirAll(libDir, 0755); err != nil {
		return fmt.Errorf("create lib dir: %w", err)
	}

	if placement.OutputPath != "" && !published {
		srcPath := resolveWorkPath(pkgInfo.SourceDir, placement.OutputPath)
		if _, err := os.Stat(srcPath); err == nil {
			if err := copyArtifact(srcPath, filepath.Join(libDir, filepath.Base(placement.OutputPath)), nil); err != nil {
				return fmt.Errorf("install library failed: %w", err)
			}
		}
		if err := copyImportLibrary(placement.ImportLib, libDir, nil); err != nil {
			return fmt.Errorf("install import library failed: %w", err)
		}
	}

	if err := os.MkdirAll(includeDir, 0755); err != nil {
		return fmt.Errorf("create include dir: %w", err)
	}

	srcDir := s.effectiveSourceDir(resolved.Node.PkgName)
	buildDir := resolveWorkPath(pkgInfo.SourceDir, pkgInfo.EffectiveBuildDir())
	return copyPublicIncludes(t, srcDir, buildDir, includeDir, nil)
}

// implibMissingAt reports whether a PE shared target's import library still
// needs publishing: LinkShared emitted one next to the artifact, but the
// destination directory does not have it.
func implibMissingAt(implib, destDir string) bool {
	if implib == "" {
		return false
	}
	if _, err := os.Stat(implib); err != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(destDir, filepath.Base(implib)))
	return err != nil
}
