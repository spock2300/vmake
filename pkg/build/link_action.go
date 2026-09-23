package build

import (
	"fmt"
	"path/filepath"

	"github.com/spock2300/vmake/pkg/api"
)

type linkAction struct {
	command    commandSpec
	signature  string
	recordPath string
	inputs     []string
	outputs    []string
}

func (s *Scheduler) toolIdentity() string {
	if s.resolvedTools == nil {
		return ""
	}
	return s.resolvedTools.CCKey()
}

func (s *Scheduler) planLink(resolved *ResolvedTarget, objs []string) (*linkAction, error) {
	target := resolved.Node.Target
	info := s.pkgs[resolved.Node.PkgName]
	workDir := info.SourceDir
	policy := LinkPolicy{
		TargetOS:      s.targetOS(resolved.Node.PkgName),
		VersionScript: resolved.VersionScript,
		ExcludeLibs:   resolved.ExcludeLibs,
		SymbolBinding: resolved.SymbolBinding,
	}
	action := &linkAction{
		recordPath: resolveWorkPath(workDir, info.OutputPath(filepath.Join("state", pathKey(target.Name())+".link.json"))),
		inputs:     append(append([]string{}, objs...), resolved.DepArtifacts...),
		outputs:    []string{resolved.OutputPath},
	}
	var err error
	if target.Prebuilt() != "" {
		source := resolveWorkPath(workDir, target.Prebuilt())
		action.inputs = append(action.inputs, source)
		operation := "copy"
		if len(target.PostLinkSteps()) == 0 {
			operation = "symlink"
		}
		action.command = commandSpec{Program: operation, Args: []string{source, resolved.OutputPath}}
	} else {
		allObjs := append(append([]string{}, objs...), resolved.DepArtifacts...)
		switch target.Kind() {
		case api.TargetBinary:
			action.command, err = s.linker.binaryCommand(allObjs, resolved.AllLinks, resolved.AllLdFlags, resolved.OutputPath, resolved.LinkerScript, policy, workDir)
		case api.TargetStatic:
			var objects []string
			for _, path := range allObjs {
				if !isLibraryArtifact(path) {
					objects = append(objects, path)
				}
			}
			action.command = s.linker.staticCommand(objects, resolved.OutputPath, workDir)
		case api.TargetShared:
			flags := append([]string{}, resolved.AllLdFlags...)
			for _, link := range resolved.AllLinks {
				flags = append(flags, "-l"+link)
			}
			action.command, err = s.linker.sharedCommand(allObjs, flags, resolved.OutputPath, policy, workDir)
		case api.TargetObject:
			if len(objs) == 0 {
				return nil, fmt.Errorf("object target requires at least one source file")
			}
			action.command = s.linker.objectCommand(objs, resolved.OutputPath, workDir)
		default:
			return nil, fmt.Errorf("target %s has unknown kind %q", resolved.Node.FullName, target.Kind())
		}
		if err != nil {
			return nil, err
		}
		if importLibrary := importLibraryPath(resolved.OutputPath, policy.TargetOS, target.Kind()); importLibrary != "" {
			action.outputs = append(action.outputs, importLibrary)
		}
	}
	action.inputs = append(action.inputs, resolved.VersionScript, resolved.LinkerScript)
	action.inputs = append(action.inputs, target.PostLinkDeps()...)
	action.outputs = append(action.outputs, postLinkOutputPaths(target, workDir, resolved.OutputPath)...)
	commands := []commandSpec{action.command}
	for _, step := range target.PostLinkSteps() {
		tool := s.resolvePostLinkTool(step.Tool)
		if tool == "" {
			return nil, fmt.Errorf("post-link tool not found: %s", step.Tool)
		}
		commands = append(commands, commandSpec{Program: tool, Args: expandPostLinkArgs(step.Args, workDir, resolved.OutputPath)})
	}
	env, err := environmentSignature(s.toolchain.CommandEnv())
	if err != nil {
		return nil, err
	}
	action.signature, err = digestValue(struct {
		Version     int
		Target      string
		Kind        api.TargetKind
		WorkDir     string
		TargetOS    string
		Tools       string
		Environment string
		Commands    []commandSpec
		Inputs      []string
		Outputs     []string
	}{buildFormatVersion, resolved.Node.FullName, target.Kind(), workDir, policy.TargetOS, s.toolIdentity(), env, commands, action.inputs, action.outputs})
	if err != nil {
		return nil, err
	}
	return action, nil
}
