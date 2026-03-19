package exec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	vlog "gitee.com/spock2300/vmake/pkg/log"
)

type RunOptions struct {
	Dir     string
	Context context.Context
	Timeout time.Duration
}

func Run(name string, args ...string) ([]byte, error) {
	return RunInDir(name, "", args...)
}

func RunInDir(name, dir string, args ...string) ([]byte, error) {
	return RunWithOptions(name, args, RunOptions{Dir: dir})
}

func RunWithOptions(name string, args []string, opts RunOptions) ([]byte, error) {
	cmdLine := formatCommandLine(name, args)
	vlog.Debug("  %s", cmdLine)

	ctx := opts.Context
	if ctx == nil && opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), opts.Timeout)
		defer cancel()
	}

	var cmd *exec.Cmd
	if ctx != nil {
		cmd = exec.CommandContext(ctx, name, args...)
	} else {
		cmd = exec.Command(name, args...)
	}

	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}

	var buf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &buf)

	err := cmd.Run()
	output := buf.Bytes()

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
