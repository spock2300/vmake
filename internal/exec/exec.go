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
	Quiet   bool
}

func Run(name string, args ...string) ([]byte, error) {
	return RunInDir(name, "", args...)
}

func RunInDir(name, dir string, args ...string) ([]byte, error) {
	return RunWithOptions(name, args, RunOptions{Dir: dir})
}

func TrimOutput(output []byte) string {
	return strings.TrimSpace(string(output))
}

func RunWithOptions(name string, args []string, opts RunOptions) ([]byte, error) {
	cmdLine := FormatCommandLine(name, args)
	vlog.Debug("%s  %s", opts.Dir, cmdLine)

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
	if opts.Quiet {
		cmd.Stdout = &buf
		cmd.Stderr = &buf
	} else {
		cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
		cmd.Stderr = io.MultiWriter(os.Stderr, &buf)
	}

	err := cmd.Run()
	output := buf.Bytes()

	if err != nil {
		return nil, fmt.Errorf("%s\n%s", cmdLine, string(output))
	}

	return output, nil
}

func buildCmd(name string, args []string, dir string, env map[string]string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), flattenEnv(env)...)
	}
	return cmd
}

func RunToStdout(dir, name string, args ...string) error {
	return RunWithEnv(dir, nil, name, args...)
}

func RunFatal(dir, name string, args ...string) {
	if err := RunToStdout(dir, name, args...); err != nil {
		vlog.Fatal("command failed: %s %s", name, strings.Join(args, " "))
	}
}

func RunWithEnv(dir string, env map[string]string, name string, args ...string) error {
	cmd := buildCmd(name, args, dir, env)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func RunWithEnvCaptured(dir string, env map[string]string, name string, args ...string) ([]byte, error) {
	cmd := buildCmd(name, args, dir, env)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %s\n%s", name, strings.Join(args, " "), string(output))
	}
	return output, nil
}

func flattenEnv(env map[string]string) []string {
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}

func FormatCommandLine(name string, args []string) string {
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
