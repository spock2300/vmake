package pipeline

import (
	"fmt"
	"path/filepath"

	"github.com/spock2300/vmake/internal/storage"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
)

type layoutInput struct {
	Local     bool
	SourceDir string
	PkgRoot   string
	Member    string
	CCKey     string
	Mode      string
	Options   map[string]any
	Material  keyMaterial
	Ready     bool
}

func layoutFor(in layoutInput) (build.PackageLayout, error) {
	member := filepath.ToSlash(in.Member)
	sourceDir := in.SourceDir
	if !in.Local {
		sourceDir = filepath.Join(in.PkgRoot, "src", filepath.FromSlash(member))
	}
	if !in.Ready {
		return build.PackageLayout{
			Dirs:  api.PkgDirs{SourceDir: sourceDir},
			Ready: false,
		}, nil
	}
	var key pkgKey
	if in.Local {
		key = newLocalPkgKey(in.CCKey, in.Mode, in.Options, in.Material)
	} else {
		key = newRemotePkgKey(in.CCKey, in.Mode, in.Options, in.Material)
	}
	buildKey, err := key.buildKeyE()
	if err != nil {
		return build.PackageLayout{}, err
	}
	if in.Local {
		return build.PackageLayout{
			Dirs: api.PkgDirs{
				SourceDir: sourceDir,
				BuildDir:  filepath.Join(sourceDir, "build", buildKey),
			},
			BuildKey: buildKey,
			Ready:    true,
		}, nil
	}
	output := filepath.Join(in.PkgRoot, "out", storage.OwnerKey(member), buildKey)
	return build.PackageLayout{
		Dirs: api.PkgDirs{
			SourceDir:  sourceDir,
			BuildDir:   filepath.Join(output, "build"),
			InstallDir: filepath.Join(output, "install"),
		},
		BuildKey: buildKey,
		Ready:    true,
	}, nil
}

func (s *sourceStage) setLayout(name string, layout build.PackageLayout) {
	value := layout
	s.layouts[name] = &value
	s.pkgDirs[name] = &value.Dirs
}

func (s *sourceStage) rebindLayout(name string, layout build.PackageLayout) error {
	old := s.layouts[name]
	if old == nil {
		if dirs := s.pkgDirs[name]; dirs != nil {
			old = &build.PackageLayout{Dirs: *dirs}
		}
	}
	changed := old == nil || old.BuildKey != layout.BuildKey || old.Dirs != layout.Dirs
	s.setLayout(name, layout)
	if err := s.preparePackageWorkspace(name); err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if err := s.applyPatchesFor(name); err != nil {
		return err
	}
	if old != nil {
		if err := rebaseKConfigSourceDirs(s.rc.ctx, name, old.Dirs.SourceDir, layout.Dirs.SourceDir); err != nil {
			return err
		}
	}
	return restoreKConfigFiles(s.rc.ctx, s.pkgDirs, map[string]bool{name: true})
}

func (s *sourceStage) keyMaterialFor(name string) (keyMaterial, error) {
	node := s.rc.ctx.DepGraph.Packages[name]
	flagsHash := packageFlagsHash(s.rc.globalFlagsHash, node)
	scriptHash, err := s.scriptHashFor(name)
	if err != nil {
		return keyMaterial{}, err
	}
	if node == nil || node.IsLocal() {
		return keyMaterial{
			commit:     s.sourceCommits[name],
			flagsHash:  flagsHash,
			scriptHash: scriptHash,
		}, nil
	}
	owner := remoteOwnerName(s.rc.ctx, name)
	version := s.sourceVersions[name]
	commit := ""
	if s.remote != nil {
		commit = s.remote.commits[owner]
		if version == "" {
			version = s.sourceVersions[owner]
		}
		if version == "" {
			if entry := s.remote.entries[owner]; entry != nil {
				version = entry.Version
			}
		}
	}
	if version == "" && node.Native != nil {
		version = node.Native.Selected
	}
	if commit == "" && node.Native != nil {
		commit = node.Native.Commit
	}
	return keyMaterial{
		version:    version,
		commit:     s.packageCommitKey(name, commit),
		flagsHash:  flagsHash,
		patchHash:  s.patchHashes[name],
		scriptHash: scriptHash,
	}, nil
}

func (s *sourceStage) makeLayout(name string, tools *build.ResolvedTools) (build.PackageLayout, error) {
	node := s.rc.ctx.DepGraph.Packages[name]
	if node == nil || node.Source == nil {
		return build.PackageLayout{}, fmt.Errorf("package %s has no source", name)
	}
	material, err := s.keyMaterialFor(name)
	if err != nil {
		return build.PackageLayout{}, err
	}
	if node.IsLocal() {
		return layoutFor(layoutInput{
			Local:     true,
			SourceDir: node.Source.Dir,
			CCKey:     tools.CCKey(),
			Mode:      s.rc.cfg.Mode,
			Options:   s.conf.allPkgOptions[name],
			Material:  material,
			Ready:     true,
		})
	}
	owner := remoteOwnerName(s.rc.ctx, name)
	member := remoteMemberPath(s.rc.ctx, name)
	tree := ""
	if s.remote != nil {
		tree = s.remote.trees[name]
		if tree == "" {
			tree = s.remote.trees[owner]
		}
	}
	if tree == "" {
		tree = remoteTreeDir(s.rc.ctx, owner)
	}
	return layoutFor(layoutInput{
		PkgRoot:  tree,
		Member:   member,
		CCKey:    tools.CCKey(),
		Mode:     s.rc.cfg.Mode,
		Options:  s.conf.allPkgOptions[name],
		Material: material,
		Ready:    true,
	})
}
