package build

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
)

func (s *Scheduler) finalizeTarget(resolved *ResolvedTarget, pkgInfo *PkgInfo, objs []string) (actionRan bool, err error) {
	if err := s.ctx.Err(); err != nil {
		return false, err
	}
	target := resolved.Node.Target
	if len(target.PostLinkDeps()) > 0 && len(target.PostLinkSteps()) == 0 {
		return false, fmt.Errorf("target %s: AddPostLinkDeps requires at least one post-link step (use AddInputs/AddOutputs for TargetVoid incremental tracking)", resolved.Node.FullName)
	}
	if target.Kind() == api.TargetVoid {
		if len(target.Inputs()) > 0 || len(target.Outputs()) > 0 {
			return s.finalizeIncrementalVoid(resolved, pkgInfo)
		}
		if err := s.buildVoidTarget(resolved); err != nil {
			return false, err
		}
		if err := s.postLink(resolved); err != nil {
			return false, err
		}
		if err := s.ctx.Err(); err != nil {
			return false, err
		}
		if err := verifyPostLinkOutputs(target, pkgInfo, resolved.OutputPath); err != nil {
			return false, err
		}
		return true, nil
	}
	action, err := s.planLink(resolved, objs)
	if err != nil {
		return false, err
	}
	prebuilt := target.Prebuilt() != ""
	if prebuilt && runtime.GOOS == "windows" {
		// A generator can replace the bytes while preserving size and mtime.
		// Windows has no change-time key in the per-scheduler hash cache.
		s.hashes.evict(action.WorkDir, action.Commands[0].Args[:1])
	}
	upToDate, _, err := s.runPlannedAction(s.ctx, action, func(ctx context.Context) ([]string, error) {
		if s.stats != nil {
			s.stats.RecordLink(resolved.Node.FullName)
		}
		if prebuilt {
			if err := realizePrebuilt(action.WorkDir, action.Commands[0]); err != nil {
				return nil, err
			}
		} else {
			vlog.Info("  LINK %s", filepath.Base(resolved.OutputPath))
			if err := s.linker.execute(ctx, action.Commands[0], resolved.OutputPath, pkgInfo.SourceDir, resolved.Node.Target.Kind() == api.TargetStatic); err != nil {
				return nil, err
			}
		}
		if err := s.runPostLinkCommands(ctx, action.Commands[1:], resolved); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, nil
	})
	if err != nil {
		return false, err
	}
	if err := s.ctx.Err(); err != nil {
		return false, err
	}
	if pkgInfo.InstallDir != "" {
		if err := s.publishTarget(resolved, pkgInfo, resolved.Placement, !upToDate); err != nil {
			return false, err
		}
	}
	return !upToDate, nil
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

func (s *Scheduler) finalizeIncrementalVoid(resolved *ResolvedTarget, pkgInfo *PkgInfo) (bool, error) {
	target := resolved.Node.Target
	if len(target.Inputs()) == 0 || len(target.Outputs()) == 0 {
		return false, fmt.Errorf("target %s: TargetVoid incremental tracking requires both AddInputs and AddOutputs", resolved.Node.FullName)
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
		return false, err
	}
	var cmdEnv map[string]string
	if s.toolchain != nil {
		cmdEnv = s.toolchain.CommandEnv()
	}
	env, err := environmentSignature(cmdEnv)
	if err != nil {
		return false, err
	}
	action := &plannedAction{
		Kind:            "void",
		Target:          resolved.Node.FullName,
		WorkDir:         pkgInfo.SourceDir,
		TargetOS:        s.targetOS(resolved.Node.PkgName),
		Tools:           s.toolIdentity(),
		Environment:     env,
		Commands:        postCommands,
		Inputs:          inputs,
		SignatureInputs: append([]string{}, inputs...),
		Outputs:         outputs,
		RecordPath:      resolveWorkPath(pkgInfo.SourceDir, pkgInfo.OutputPath(filepath.Join("state", pathKey(target.Name())+".void.json"))),
	}
	upToDate, _, err := s.runPlannedAction(s.ctx, action, func(ctx context.Context) ([]string, error) {
		if err := s.buildVoidTarget(resolved); err != nil {
			return nil, err
		}
		if err := s.runPostLinkCommands(ctx, postCommands, resolved); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := verifyPostLinkOutputs(target, pkgInfo, resolved.OutputPath); err != nil {
			return nil, err
		}
		return nil, nil
	})
	return !upToDate && err == nil, err
}

func realizePrebuilt(workDir string, cmd commandSpec) error {
	src := cmd.Args[0] // already resolved by planLink
	dst := resolveWorkPath(workDir, cmd.Args[1])
	if rel, err := filepath.Rel(src, dst); err == nil && rel == "." {
		return fmt.Errorf("prebuilt source and output must differ: %s", src)
	}
	info, err := statRegularFile(src)
	if err != nil {
		return fmt.Errorf("prebuilt source %s: %w", src, err)
	}
	vlog.Info("  PREBUILT %s", filepath.Base(dst))
	switch cmd.Program {
	case "symlink":
		absolute, err := filepath.Abs(src)
		if err != nil {
			return err
		}
		if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
			return err
		}
		return fs.EnsureSymlink(dst, absolute)
	case "copy":
		return copyPrebuilt(src, dst, info.Mode())
	default:
		return fmt.Errorf("unknown prebuilt operation %q", cmd.Program)
	}
}

// Publish only a complete copy. Renaming the staged file also replaces an old
// symlink itself without opening or modifying its target.
func copyPrebuilt(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	staged, err := os.CreateTemp(filepath.Dir(dst), ".prebuilt-*.tmp")
	if err != nil {
		return err
	}
	name := staged.Name()
	defer os.Remove(name)
	if runtime.GOOS == "windows" {
		mode |= 0200 // A generated output must remain replaceable on Windows.
	}
	_, err = io.Copy(staged, in)
	if err == nil {
		err = staged.Chmod(mode)
	}
	closeErr := staged.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return fs.RenameRetry(name, dst)
}

func (s *Scheduler) buildVoidTarget(resolved *ResolvedTarget) (err error) {
	defer scriptcall.Recover(&err)
	fn := resolved.Node.Target.BuildFunc()
	if fn == nil {
		return nil
	}

	pkg, ok := s.packages[resolved.Node.PkgName]
	if !ok || pkg == nil {
		return fmt.Errorf("package %s is not activated", resolved.Node.PkgName)
	}
	s.populateDepsFromGraph(pkg, resolved.Node)

	pkg.SetGlobalFlags(s.globalCFlags, s.globalCxxFlags, s.globalLdFlags, s.globalLinks)

	if err := os.MkdirAll(pkg.BuildDir(), 0755); err != nil {
		return fmt.Errorf("create build dir: %s: %w", pkg.BuildDir(), err)
	}

	if s.stats != nil {
		s.stats.RecordCallback()
	}
	if err := fn(pkg); err != nil {
		return err
	}

	s.updateVoidLibDirs(resolved, pkg)
	return nil
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
