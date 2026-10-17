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
	kind := t.Kind()
	targetOS := s.targetOS(resolved.Node.PkgName)

	if kind == api.TargetVoid || kind == api.TargetObject || t.IsTest() {
		return nil
	}

	libDir := filepath.Join(pkgInfo.InstallDir, "lib")
	includeDir := filepath.Join(pkgInfo.InstallDir, "include")
	published := false

	if resolved.OutputPath != "" {
		srcPath := resolveWorkPath(pkgInfo.SourceDir, resolved.OutputPath)
		dest := filepath.Join(libDir, filepath.Base(resolved.OutputPath))
		if info, err := os.Stat(dest); err == nil && info.Mode().IsRegular() &&
			sameFileContent(srcPath, dest) && !implibMissing(srcPath, libDir, targetOS, kind) {
			vlog.Info("  SKIP (already published)")
			published = true
		}
	}

	if err := os.MkdirAll(libDir, 0755); err != nil {
		return fmt.Errorf("create lib dir: %w", err)
	}

	if resolved.OutputPath != "" && !published {
		srcPath := resolveWorkPath(pkgInfo.SourceDir, resolved.OutputPath)
		if _, err := os.Stat(srcPath); err == nil {
			if err := copyArtifact(srcPath, filepath.Join(libDir, filepath.Base(resolved.OutputPath)), nil); err != nil {
				return fmt.Errorf("install library failed: %w", err)
			}
		}
		if err := copyImportLibrary(srcPath, libDir, targetOS, kind, nil); err != nil {
			return fmt.Errorf("install import library failed: %w", err)
		}
	}

	if err := os.MkdirAll(includeDir, 0755); err != nil {
		return fmt.Errorf("create include dir: %w", err)
	}

	srcDir := s.effectiveSourceDir(resolved.Node.PkgName)
	return copyPublicIncludes(t, srcDir, includeDir, nil)
}

// implibMissing reports whether a PE shared target's import library still needs
// publishing: LinkShared emitted one next to the artifact, but the destination
// directory does not have it. Guards the "already published" fast path, which
// otherwise compares only the DLL and never repairs a deleted implib.
func implibMissing(srcPath, destDir, targetOS string, kind api.TargetKind) bool {
	implib := importLibraryPath(srcPath, targetOS, kind)
	if implib == "" {
		return false
	}
	if _, err := os.Stat(implib); err != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(destDir, filepath.Base(implib)))
	return err != nil
}
