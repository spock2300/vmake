package build

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
)

func (s *Scheduler) finalizeTarget(resolved *ResolvedTarget, pkgInfo *PkgInfo, objs []string) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	target := resolved.Node.Target
	if len(target.PostLinkDeps()) > 0 && len(target.PostLinkSteps()) == 0 {
		return fmt.Errorf("target %s: AddPostLinkDeps requires at least one post-link step (use AddInputs/AddOutputs for TargetVoid incremental tracking)", resolved.Node.FullName)
	}
	if target.Kind() == api.TargetVoid {
		if len(target.Inputs()) > 0 || len(target.Outputs()) > 0 {
			return s.finalizeIncrementalVoid(resolved, pkgInfo)
		}
		if err := s.buildVoidTarget(resolved); err != nil {
			return err
		}
		if err := s.postLink(resolved); err != nil {
			return err
		}
		if err := s.ctx.Err(); err != nil {
			return err
		}
		return verifyPostLinkOutputs(target, pkgInfo, resolved.OutputPath)
	}
	action, err := s.planLink(resolved, objs)
	if err != nil {
		return err
	}
	decision := checkAction(s.hashes, action.recordPath, action.signature, pkgInfo.SourceDir, action.inputs, action.outputs)
	if !decision.UpToDate {
		if s.stats != nil {
			s.stats.RecordLink(resolved.Node.FullName)
		}
		if s.explain {
			vlog.Info("  EXPLAIN %s: %s", resolved.Node.FullName, decision.Reason)
		}
		if err := invalidateActionRecord(action.recordPath); err != nil {
			return err
		}
		if resolved.Node.Target.Prebuilt() != "" {
			if err := s.realizePrebuilt(resolved); err != nil {
				return err
			}
		} else {
			vlog.Info("  LINK %s", filepath.Base(resolved.OutputPath))
			if err := s.linker.execute(action.command, resolved.OutputPath, pkgInfo.SourceDir, resolved.Node.Target.Kind() == api.TargetStatic); err != nil {
				return err
			}
		}
		if err := s.postLink(resolved); err != nil {
			return err
		}
		if err := s.ctx.Err(); err != nil {
			return err
		}
		if err := saveActionRecord(s.hashes, action.recordPath, action.signature, pkgInfo.SourceDir, action.inputs, action.outputs); err != nil {
			return err
		}
	} else if s.stats != nil {
		s.stats.RecordUpToDate()
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if pkgInfo.InstallDir != "" {
		return s.publishTarget(resolved, pkgInfo)
	}
	return nil
}

func verifyPostLinkOutputs(target *api.Target, pkgInfo *PkgInfo, outputPath string) error {
	for _, output := range expandPostLinkArgs(target.PostLinkOutputs(), pkgInfo.SourceDir, outputPath) {
		path := pkgInfo.ResolvePath(output)
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("post-link output %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("post-link output is not a regular file: %s", path)
		}
	}
	return nil
}

func (s *Scheduler) finalizeIncrementalVoid(resolved *ResolvedTarget, pkgInfo *PkgInfo) error {
	target := resolved.Node.Target
	if len(target.Inputs()) == 0 || len(target.Outputs()) == 0 {
		return fmt.Errorf("target %s: TargetVoid incremental tracking requires both AddInputs and AddOutputs", resolved.Node.FullName)
	}
	buildDir := pkgInfo.EffectiveBuildDir()
	var inputs []string
	for _, in := range target.Inputs() {
		inputs = append(inputs, expandBuildDirToken(buildDir, in))
	}
	for _, dep := range target.PostLinkDeps() {
		inputs = append(inputs, expandBuildDirToken(buildDir, dep))
	}
	var outputs []string
	for _, out := range target.Outputs() {
		outputs = append(outputs, expandBuildDirToken(buildDir, out))
	}
	outputs = append(outputs, postLinkOutputPaths(target, pkgInfo.SourceDir, resolved.OutputPath)...)

	postCommands, err := s.postLinkCommands(target, pkgInfo.SourceDir, resolved.OutputPath)
	if err != nil {
		return err
	}
	var cmdEnv map[string]string
	if s.toolchain != nil {
		cmdEnv = s.toolchain.CommandEnv()
	}
	env, err := environmentSignature(cmdEnv)
	if err != nil {
		return err
	}
	recordPath := resolveWorkPath(pkgInfo.SourceDir, pkgInfo.OutputPath(filepath.Join("state", pathKey(target.Name())+".void.json")))
	signature, err := digestValue(struct {
		Version     int
		Target      string
		Kind        api.TargetKind
		WorkDir     string
		Tools       string
		Environment string
		Commands    []commandSpec
		Inputs      []string
		Outputs     []string
	}{actionRecordVersion, resolved.Node.FullName, target.Kind(), pkgInfo.SourceDir, s.toolIdentity(), env, postCommands, inputs, outputs})
	if err != nil {
		return err
	}
	decision := checkAction(s.hashes, recordPath, signature, pkgInfo.SourceDir, inputs, outputs)
	if decision.UpToDate {
		if s.stats != nil {
			s.stats.RecordUpToDate()
		}
		return nil
	}
	if s.explain {
		vlog.Info("  EXPLAIN %s: %s", resolved.Node.FullName, decision.Reason)
	}
	if err := invalidateActionRecord(recordPath); err != nil {
		return err
	}
	if err := s.buildVoidTarget(resolved); err != nil {
		return err
	}
	if err := s.postLink(resolved); err != nil {
		return err
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if err := verifyPostLinkOutputs(target, pkgInfo, resolved.OutputPath); err != nil {
		return err
	}
	return saveActionRecord(s.hashes, recordPath, signature, pkgInfo.SourceDir, inputs, outputs)
}

func (s *Scheduler) realizePrebuilt(resolved *ResolvedTarget) error {
	src := resolved.Node.Target.Prebuilt()
	pkgInfo := s.pkgs[resolved.Node.PkgName]
	workDir := pkgInfo.SourceDir
	absDst := resolveWorkPath(workDir, resolved.OutputPath)
	absSrc := pkgInfo.ResolvePath(src)

	if _, err := os.Stat(absSrc); err != nil {
		return fmt.Errorf("prebuilt file not found: %s: %w", absSrc, err)
	}

	vlog.Info("  PREBUILT %s", filepath.Base(absDst))
	if filepath.Clean(absSrc) == filepath.Clean(absDst) {
		return fmt.Errorf("prebuilt source and output must differ: %s", absSrc)
	}
	if err := os.Remove(absDst); err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(resolved.Node.Target.PostLinkSteps()) == 0 {
		srcPath, err := filepath.Abs(absSrc)
		if err != nil {
			return err
		}
		return fs.EnsureSymlink(absDst, srcPath)
	}
	return CopyFile(absSrc, absDst)
}

func (s *Scheduler) buildVoidTarget(resolved *ResolvedTarget) (err error) {
	defer scriptcall.Recover(&err)
	fn := resolved.Node.Target.BuildFunc()
	if fn == nil {
		return nil
	}

	pkg := s.ensurePackageForVoid(resolved.Node)
	s.populateDepsFromGraph(pkg, resolved.Node)

	pkg.SetGlobalFlags(s.globalCFlags, s.globalCxxFlags, s.globalLdFlags, s.globalLinks)

	if err := os.MkdirAll(pkg.BuildDir(), 0755); err != nil {
		return fmt.Errorf("create build dir: %s: %w", pkg.BuildDir(), err)
	}

	if err := fn(pkg); err != nil {
		return err
	}

	s.updateVoidLibDirs(resolved, pkg)
	return nil
}

func (s *Scheduler) ensurePackageForVoid(node *BuildNode) *api.Package {
	pkg := s.packages[node.PkgName]
	if pkg != nil {
		return pkg
	}

	pkgInfo := s.pkgs[node.PkgName]
	pkg = api.NewPackage()
	buildDir := pkgInfo.BuildDir
	if buildDir == "" {
		buildDir = BuildPath(pkgInfo.SourceDir, pkgInfo.BuildKey, "")
	}
	pkg.SetDirs(api.PkgDirs{
		SourceDir: pkgInfo.SourceDir,
		BuildDir:  buildDir,
	})
	pkg.SetToolchain(s.toolchain)
	pkg.SetPlatform(s.platform)
	pkg.SetSrcDir(pkgInfo.SourceDir)
	cfgVals := map[string]any{api.ModeOptionName: s.mode}
	if opts, ok := s.pkgOptions[node.PkgName]; ok {
		for k, v := range opts {
			cfgVals[k] = v
		}
	}
	pkg.SetCfgVals(cfgVals)
	s.packages[node.PkgName] = pkg
	return pkg
}

func (s *Scheduler) updateVoidLibDirs(resolved *ResolvedTarget, pkg *api.Package) {
	pkgName := resolved.Node.PkgName
	for _, dep := range pkg.Deps() {
		if dep.Name == pkgName {
			dep.UpdateLibDir()
		}
	}
	for _, otherPkg := range s.packages {
		if dep, ok := otherPkg.Deps()[pkgName]; ok {
			dep.UpdateLibDir()
		}
	}
}

func (s *Scheduler) populateDepsFromGraph(pkg *api.Package, node *BuildNode) {
	for _, depFullName := range node.Deps {
		depNode := s.graph.Nodes[depFullName]
		if depNode == nil {
			continue
		}
		depPkgName := depNode.PkgName
		if _, ok := pkg.Deps()[depPkgName]; ok {
			continue
		}
		pkgInfo := s.pkgs[depPkgName]
		if pkgInfo == nil || pkgInfo.InstallDir == "" {
			continue
		}
		var depLibs []string
		depLibs = append(depLibs, depNode.Target.ProvidedLibs()...)
		ip := api.NewInstalledPackage(depPkgName, "", pkgInfo.InstallDir, depLibs)
		pkg.SetDep(depPkgName, ip)
	}
}

func sameFileContent(a, b string) bool {
	ha, err := FileHash(a)
	if err != nil {
		return false
	}
	hb, err := FileHash(b)
	if err != nil {
		return false
	}
	return ha == hb
}
