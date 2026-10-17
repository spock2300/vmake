package build

import (
	"fmt"
	"path/filepath"
	"strings"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
)

func (s *Scheduler) postLinkCommands(target *api.Target, workDir, outputPath string) ([]commandSpec, error) {
	steps := target.PostLinkSteps()
	commands := make([]commandSpec, 0, len(steps))
	for _, step := range steps {
		tool := s.resolvePostLinkTool(step.Tool)
		if tool == "" {
			return nil, fmt.Errorf("post-link tool not found: %s", step.Tool)
		}
		commands = append(commands, commandSpec{Program: tool, Args: expandPostLinkArgs(step.Args, workDir, outputPath)})
	}
	return commands, nil
}

func (s *Scheduler) postLink(resolved *ResolvedTarget) error {
	workDir := s.pkgs[resolved.Node.PkgName].SourceDir
	commands, err := s.postLinkCommands(resolved.Node.Target, workDir, resolved.OutputPath)
	if err != nil {
		return err
	}
	for _, command := range commands {
		vlog.Info("  %s %s", filepath.Base(command.Program), strings.Join(command.Args, " "))
		if _, err := iexec.RunWithOptions(command.Program, command.Args, iexec.RunOptions{Dir: workDir, Env: s.toolchain.CommandEnv(), Context: s.ctx}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scheduler) resolvePostLinkTool(name string) string {
	switch name {
	case "objcopy":
		return s.resolvedTools.OBJCOPY
	case "size":
		return s.resolvedTools.SIZE
	case "objdump":
		return s.resolvedTools.OBJDUMP
	case "nm":
		return s.resolvedTools.NM
	case "strip":
		return s.resolvedTools.STRIP
	}
	return ""
}
