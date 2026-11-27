package build

import (
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/spock2300/vmake/pkg/api"
)

func (s *Scheduler) toolIdentity() string {
	if s.resolvedTools == nil {
		return ""
	}
	return s.resolvedTools.CCKey()
}

func (s *Scheduler) planLink(resolved *ResolvedTarget, objs []string) (*plannedAction, error) {
	target := resolved.Node.Target
	info := s.pkgs[resolved.Node.PkgName]
	workDir := info.SourceDir
	policy := LinkPolicy{
		TargetOS:      s.targetOS(resolved.Node.PkgName),
		VersionScript: resolved.VersionScript,
		ExcludeLibs:   resolved.ExcludeLibs,
		SymbolBinding: resolved.SymbolBinding,
	}
	buildInputs := append(append([]string{}, objs...), resolved.DepArtifacts...)
	action := &plannedAction{
		Kind:       "link",
		Target:     resolved.Node.FullName,
		WorkDir:    workDir,
		TargetOS:   policy.TargetOS,
		Tools:      s.toolIdentity(),
		RecordPath: resolveWorkPath(workDir, info.OutputPath(filepath.Join("state", pathKey(target.Name())+".link.json"))),
		Inputs:     append([]string{}, buildInputs...),
		StatInputs: buildInputs,
		Outputs:    []string{resolved.OutputPath},
	}
	var linkCommand commandSpec
	var err error
	if target.Prebuilt() != "" {
		source := info.ResolvePath(target.Prebuilt())
		action.Inputs = append(action.Inputs, source)
		operation := "copy"
		if runtime.GOOS != "windows" {
			if len(target.PostLinkSteps()) == 0 {
				operation = "symlink"
			}
			if buildProductPath(info, workDir, source) {
				action.StatInputs = append(action.StatInputs, source)
			}
		}
		linkCommand = commandSpec{Program: operation, Args: []string{source, resolved.OutputPath}}
	} else {
		allObjs := append(append([]string{}, objs...), resolved.DepArtifacts...)
		switch target.Kind() {
		case api.TargetBinary:
			linkCommand, err = s.linker.binaryCommand(binaryLinkInput{
				objs:         allObjs,
				libs:         resolved.AllLinks,
				ldflags:      resolved.AllLdFlags,
				outputPath:   resolved.OutputPath,
				linkerScript: resolved.LinkerScript,
			}, policy, workDir)
		case api.TargetStatic:
			var objects []string
			for _, path := range allObjs {
				if !isLibraryArtifact(path) {
					objects = append(objects, path)
				}
			}
			linkCommand = s.linker.staticCommand(objects, resolved.OutputPath, workDir)
		case api.TargetShared:
			flags := append([]string{}, resolved.AllLdFlags...)
			for _, link := range resolved.AllLinks {
				flags = append(flags, "-l"+link)
			}
			linkCommand, err = s.linker.sharedCommand(allObjs, flags, resolved.OutputPath, policy, workDir)
		case api.TargetObject:
			if len(objs) == 0 {
				return nil, fmt.Errorf("object target requires at least one source file")
			}
			linkCommand = s.linker.objectCommand(objs, resolved.OutputPath, workDir)
		default:
			return nil, fmt.Errorf("target %s has unknown kind %q", resolved.Node.FullName, target.Kind())
		}
		if err != nil {
			return nil, err
		}
		linkCommand = linkCommandForHost(runtime.GOOS, workDir, resolveWorkPath(workDir, info.RspDir()), linkCommand)
		if importLibrary := importLibraryPath(resolved.OutputPath, policy.TargetOS, target.Kind()); importLibrary != "" {
			action.Outputs = append(action.Outputs, importLibrary)
		}
	}
	action.Inputs = append(action.Inputs, resolved.VersionScript, resolved.LinkerScript)
	for _, dep := range target.PostLinkDeps() {
		action.Inputs = append(action.Inputs, expandBuildDirToken(info.EffectiveBuildDir(), dep))
	}
	action.Outputs = append(action.Outputs, postLinkOutputPaths(target, workDir, resolved.OutputPath)...)
	postCommands, err := s.postLinkCommands(target, workDir, resolved.OutputPath)
	if err != nil {
		return nil, err
	}
	action.Commands = append([]commandSpec{linkCommand}, postCommands...)
	action.SignatureInputs = append([]string{}, action.Inputs...)
	env, err := environmentSignature(s.toolchain.CommandEnv())
	if err != nil {
		return nil, err
	}
	action.Environment = env
	return action, nil
}

func linkCommandForHost(hostOS, workDir, rspDir string, command commandSpec) commandSpec {
	dir := responseFileDir(hostOS, rspDir)
	if dir == "" || len(command.Args) == 0 {
		return command
	}
	file := responseFileFor(dir, command.Args)
	return commandSpec{Program: command.Program, Args: []string{responseFileArg(hostOS, workDir, file)}, response: file}
}

// buildProductPath reports whether path was produced by the package's build.
func buildProductPath(info *PkgInfo, workDir, path string) bool {
	if info == nil || path == "" {
		return false
	}
	clean := filepath.Clean(path)
	for _, dir := range []string{info.EffectiveBuildDir(), info.InstallDir} {
		if dir == "" {
			continue
		}
		if hasPathPrefix(clean, filepath.Clean(resolveWorkPath(workDir, dir))) {
			return true
		}
	}
	return false
}
