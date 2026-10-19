package build

import (
	"fmt"
	"path/filepath"
	"strconv"
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
		out, err := iexec.RunWithOptions(command.Program, command.Args, iexec.RunOptions{Dir: workDir, Env: s.toolchain.CommandEnv(), Context: s.ctx, CaptureStderr: true})
		if err != nil {
			return err
		}
		if isSizeTool(command.Program) {
			if size := parseBerkeleySize(string(out)); size != nil {
				s.stats.RecordTargetSize(resolved.Node.FullName, size)
			}
		}
	}
	return nil
}

func isSizeTool(program string) bool {
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(program)), ".exe")
	return base == "size" || strings.HasSuffix(base, "-size")
}

func parseBerkeleySize(output string) *TargetSize {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		text, err1 := strconv.ParseInt(fields[0], 10, 64)
		data, err2 := strconv.ParseInt(fields[1], 10, 64)
		bss, err3 := strconv.ParseInt(fields[2], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		total := text + data + bss
		if len(fields) >= 4 {
			if dec, err := strconv.ParseInt(fields[3], 10, 64); err == nil {
				total = dec
			}
		}
		return &TargetSize{
			Text:  text,
			Data:  data,
			Bss:   bss,
			Total: total,
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
