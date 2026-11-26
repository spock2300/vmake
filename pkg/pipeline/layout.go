package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
)

type layoutInput struct {
	Local          bool
	SourceDir      string
	SourceOverride string
	PkgRoot        string
	Member         string
	CCKey          string
	Mode           string
	Options        map[string]any
	Facts          LayoutFacts
	Platform       api.Platform
	Ready          bool
}

func layoutFor(in layoutInput) (build.PackageLayout, error) {
	member := filepath.ToSlash(in.Member)
	sourceDir := in.SourceDir
	if !in.Local {
		if in.SourceOverride != "" {
			sourceDir = in.SourceOverride
		} else {
			sourceDir = filepath.Join(in.PkgRoot, "src", filepath.FromSlash(member))
		}
	}
	if !in.Ready {
		return build.PackageLayout{
			Dirs:     api.PkgDirs{SourceDir: sourceDir},
			Platform: in.Platform,
			Ready:    false,
		}, nil
	}
	var key pkgKey
	if in.Local {
		key = newLocalPkgKey(in.CCKey, in.Mode, in.Options, in.Facts)
	} else {
		key = newRemotePkgKey(in.CCKey, in.Mode, in.Options, in.Facts)
	}
	buildKey, err := key.buildKeyE()
	if err != nil {
		return build.PackageLayout{}, err
	}
	if in.Local {
		return build.PackageLayout{
			Dirs: api.PkgDirs{
				SourceDir: sourceDir,
				BuildDir:  build.BuildPath(sourceDir, buildKey, ""),
			},
			BuildKey: buildKey,
			Platform: in.Platform,
			Ready:    true,
		}, nil
	}
	output := filepath.Join(in.PkgRoot, "out", build.MemberDir(member), build.KeyDir(buildKey))
	return build.PackageLayout{
		Dirs: api.PkgDirs{
			SourceDir:  sourceDir,
			BuildDir:   filepath.Join(output, "build"),
			InstallDir: filepath.Join(output, "install"),
		},
		BuildKey: buildKey,
		Platform: in.Platform,
		Ready:    true,
	}, nil
}

func (s *sourceStage) setLayout(name string, layout build.PackageLayout) {
	value := layout
	s.layouts[name] = &value
	s.pkgDirs[name] = &value.Dirs
}

func (s *sourceStage) setPackageLayout(name string, tools *build.ResolvedTools) error {
	layout, err := s.makeLayout(name, tools)
	if err != nil {
		return err
	}
	s.setLayout(name, layout)
	return nil
}

func (s *sourceStage) finalizePackageSource(name, sourceRoot string, tools *build.ResolvedTools) error {
	if err := s.selectPackageSourceRoot(name, sourceRoot); err != nil {
		return fmt.Errorf("select source for %s: %w", name, err)
	}
	return s.setPackageLayout(name, tools)
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
	if layout.Dirs.InstallDir != "" && (old == nil || old.Dirs.InstallDir != layout.Dirs.InstallDir) {
		if err := clearInstallDir(name, layout.Dirs.InstallDir); err != nil {
			return err
		}
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

// layoutFactsFor derives the layout facts from live source state; the
// inspection derives the same value from the lock.
func (s *sourceStage) layoutFactsFor(name string) (LayoutFacts, error) {
	node := s.rc.ctx.DepGraph.Packages[name]
	flagsHash := packageFlagsHash(s.rc.globalFlagsHash, node)
	if node == nil || node.IsLocal() {
		sourceLink := ""
		if node != nil && node.Source != nil && node.Source.Link != nil {
			sourceLink = node.Source.Link.Path
		}
		return LayoutFacts{
			Commit:     s.sourceCommits[name],
			SourceLink: sourceLink,
			FlagsHash:  flagsHash,
		}, nil
	}
	sourceLink := ""
	if node.Source != nil && node.Source.Link != nil {
		sourceLink = node.Source.Link.Path
	}
	if node.IsNative() {
		cID := node.Container
		c := s.rc.ctx.DepGraph.Containers[cID]
		version := s.containerVersions[cID]
		commit := s.containerCommits[cID]
		if version == "" && c != nil {
			version = c.Version
		}
		if commit == "" && c != nil {
			commit = c.Commit
		}
		if node.HasOwnGit() && s.sourceVersions[name] != "" {
			if version != "" {
				version = version + "+" + s.sourceVersions[name]
			} else {
				version = s.sourceVersions[name]
			}
		}
		return LayoutFacts{
			Version:    version,
			Commit:     s.packageCommitKey(name, commit),
			FlagsHash:  flagsHash,
			PatchHash:  s.patchHashes[name],
			SourceLink: sourceLink,
		}, nil
	}
	version := s.sourceVersions[name]
	commit := s.sourceCommits[name]
	return LayoutFacts{
		Version:    version,
		Commit:     commit,
		FlagsHash:  flagsHash,
		PatchHash:  s.patchHashes[name],
		SourceLink: sourceLink,
	}, nil
}

func (s *sourceStage) makeLayout(name string, tools *build.ResolvedTools) (build.PackageLayout, error) {
	node := s.rc.ctx.DepGraph.Packages[name]
	if node == nil || node.Source == nil {
		return build.PackageLayout{}, fmt.Errorf("package %s has no source", name)
	}
	platform, err := PackagePlatform(s.rc.ctx, name)
	if err != nil {
		return build.PackageLayout{}, fmt.Errorf("package %s platform: %w", name, err)
	}
	ready := tools != nil && s.rc.cfg != nil
	ccKey := ""
	mode := ""
	var material LayoutFacts
	if ready {
		var err error
		material, err = s.layoutFactsFor(name)
		if err != nil {
			return build.PackageLayout{}, err
		}
		ccKey = tools.CCKey()
		mode = s.rc.cfg.Mode
	}
	if node.IsLocal() {
		return layoutFor(layoutInput{
			Local:     true,
			SourceDir: node.Source.Dir,
			CCKey:     ccKey,
			Mode:      mode,
			Options:   s.conf.allPkgOptions[name],
			Facts:     material,
			Platform:  platform,
			Ready:     ready,
		})
	}
	loc := s.rc.ctx.packageLocation(name, node)
	tree := ""
	if node.IsNative() {
		tree = s.containerTrees[node.Container]
	} else if !loc.Linked && s.sourceSeeds[name] != "" {
		tree = filepath.Dir(s.sourceSeeds[name])
	}
	if tree == "" {
		tree = loc.OwnerTreeRoot
	}
	sourceOverride := ""
	if loc.ContainerLinked || loc.ContainerLocalPath || (node.IsRegistry() && loc.Linked) {
		sourceOverride = loc.PackageRoot
	}
	return layoutFor(layoutInput{
		PkgRoot:        tree,
		SourceOverride: sourceOverride,
		Member:         loc.Member,
		CCKey:          ccKey,
		Mode:           mode,
		Options:        s.conf.allPkgOptions[name],
		Facts:          material,
		Platform:       platform,
		Ready:          ready,
	})
}

// clearRemoteInstallDirs removes stale published files from remote package
// install staging before OnBuild runs. Buildscript text no longer rotates the
// build directory, so files published by a previous script must not survive
// when the script stops declaring them.
func clearRemoteInstallDirs(layouts map[string]*build.PackageLayout) error {
	names := make([]string, 0, len(layouts))
	for name := range layouts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		layout := layouts[name]
		if layout == nil || layout.Dirs.InstallDir == "" {
			continue
		}
		if err := clearInstallDir(name, layout.Dirs.InstallDir); err != nil {
			return err
		}
	}
	return nil
}

func clearInstallDir(name, dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clear install dir for %s: %w", name, err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create install dir for %s: %w", name, err)
	}
	return nil
}
