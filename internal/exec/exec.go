package exec

import (
	"fmt"
	"os/exec"
	"strings"

	vlog "gitee.com/spock2300/vmake/pkg/log"
)

func Run(name string, args ...string) ([]byte, error) {
	return RunInDir(name, "", args...)
}

func RunInDir(name, dir string, args ...string) ([]byte, error) {
	cmdLine := formatCommandLine(name, args)
	vlog.Debug("  %s", cmdLine)

	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s\n%s", cmdLine, string(output))
	}

	return output, nil
}

func formatCommandLine(name string, args []string) string {
	var sb strings.Builder
	sb.WriteString(name)

	for _, arg := range args {
		sb.WriteByte(' ')
		if strings.ContainsAny(arg, " \t\"'\\") {
			sb.WriteString(fmt.Sprintf("%q", arg))
		} else {
			sb.WriteString(arg)
		}
	}

	return sb.String()
}
