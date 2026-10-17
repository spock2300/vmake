package build

import (
	"fmt"
	"path/filepath"
	"strings"

	iexec "github.com/spock2300/vmake/internal/exec"
	vlog "github.com/spock2300/vmake/pkg/log"
)

func (s *Scheduler) postLink(resolved *ResolvedTarget) error {
	steps := resolved.Node.Target.PostLinkSteps()
	if len(steps) == 0 {
		return nil
	}

	workDir := s.pkgs[resolved.Node.PkgName].SourceDir

	for _, step := range steps {
		tool := s.resolvePostLinkTool(step.Tool)
		if tool == "" {
			return fmt.Errorf("post-link tool not found: %s", step.Tool)
		}

		args := expandPostLinkArgs(step.Args, workDir, resolved.OutputPath)

		vlog.Info("  %s %s", filepath.Base(tool), strings.Join(args, " "))
		if _, err := iexec.RunWithOptions(tool, args, iexec.RunOptions{Dir: workDir, Env: s.toolchain.CommandEnv(), Context: s.ctx}); err != nil {
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
