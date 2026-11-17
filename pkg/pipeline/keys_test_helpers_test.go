package pipeline

import (
	"path/filepath"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
)

func localPkgKeyFromParts(ccKey, mode string, opts map[string]any, globalFlagsHash, commit string) pkgKey {
	return newLocalPkgKey(ccKey, mode, opts, LayoutFacts{Commit: commit, FlagsHash: globalFlagsHash})
}

func remotePkgKeyFromParts(ccKey, mode string, opts map[string]any, version, commit, globalFlagsHash, patchHash string) pkgKey {
	return newRemotePkgKey(ccKey, mode, opts, LayoutFacts{
		Version:   version,
		Commit:    commit,
		FlagsHash: globalFlagsHash,
		PatchHash: patchHash,
	})
}

func (k pkgKey) buildKey() string {
	key, _ := build.BuildKeyE(k.ccKey, k.mode, k.options, k.extra)
	return key
}

func makeLocalPkgDirs(scriptDir string, key pkgKey) *api.PkgDirs {
	return &api.PkgDirs{
		SourceDir: scriptDir,
		BuildDir:  filepath.Join(scriptDir, "build", build.KeyDir(key.buildKey())),
	}
}

func makeRemotePkgDirs(pkgRoot, member string, key pkgKey) *api.PkgDirs {
	member = filepath.ToSlash(member)
	output := filepath.Join(pkgRoot, "out", build.MemberDir(member), build.KeyDir(key.buildKey()))
	return &api.PkgDirs{
		SourceDir:  filepath.Join(pkgRoot, "src", filepath.FromSlash(member)),
		BuildDir:   filepath.Join(output, "build"),
		InstallDir: filepath.Join(output, "install"),
	}
}

func (s *sourceStage) selectPackageSource(name string) error {
	root := ""
	if dirs := s.pkgDirs[name]; dirs != nil {
		root = dirs.SourceDir
	}
	return s.selectPackageSourceRoot(name, root)
}
