package exec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type Logger interface {
	Debug(format string, args ...any)
	Error(format string, args ...any)
	Fatal(format string, args ...any)
}

type noopLogger struct{}

func (noopLogger) Debug(string, ...any) {}

func (noopLogger) Error(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

func (noopLogger) Fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

var logger Logger = noopLogger{}

func SetLogger(l Logger) {
	logger = l
}

type RunOptions struct {
	Dir     string
	Env     map[string]string
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
	logger.Debug("%s  %s", opts.Dir, cmdLine)

	ctx := opts.Context
	if opts.Timeout > 0 {
		if ctx == nil {
			ctx = context.Background()
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	cmd := buildCmd(ctx, name, args, opts.Dir, opts.Env)

	var buf bytes.Buffer
	if opts.Quiet {
		cmd.Stdout = &buf
		cmd.Stderr = &buf
	} else {
		cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
		cmd.Stderr = os.Stderr
	}

	err := cmd.Run()
	if err != nil && ctx != nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	output := buf.Bytes()

	if err != nil {
		if opts.Quiet {
			return nil, fmt.Errorf("%s: %w\n%s", cmdLine, err, string(output))
		}
		return nil, fmt.Errorf("%s: %w\n%s", cmdLine, err, TrimOutput(output))
	}

	return output, nil
}

func buildCmd(ctx context.Context, name string, args []string, dir string, env map[string]string) *exec.Cmd {
	var cmd *exec.Cmd
	if ctx != nil {
		cmd = exec.CommandContext(ctx, name, args...)
		if ctx.Done() != nil {
			configureCancellation(cmd)
		}
	} else {
		cmd = exec.Command(name, args...)
	}
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), flattenEnv(env)...)
		if filepath.Base(name) == name && (runtime.GOOS != "windows" || !strings.ContainsAny(name, `:\/`)) {
			cmd.Path, cmd.Err = lookPathEnv(name, cmd.Env)
		}
	}
	return cmd
}

func RunToStdout(dir, name string, args ...string) error {
	return RunWithEnv(dir, nil, name, args...)
}

func RunFatal(dir, name string, args ...string) {
	if err := RunToStdout(dir, name, args...); err != nil {
		logger.Fatal("command failed: %v", err)
	}
}

func RunWithEnv(dir string, env map[string]string, name string, args ...string) error {
	return RunWithEnvContext(nil, dir, env, name, args...)
}

func RunWithEnvContext(ctx context.Context, dir string, env map[string]string, name string, args ...string) error {
	cmd := buildCmd(ctx, name, args, dir, env)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil && ctx != nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("%s: %w", FormatCommandLine(name, args), err)
	}
	return nil
}

func LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func RunWithEnvCaptured(dir string, env map[string]string, name string, args ...string) ([]byte, error) {
	cmd := buildCmd(nil, name, args, dir, env)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w\n%s", FormatCommandLine(name, args), err, string(output))
	}
	return output, nil
}

func flattenEnv(env map[string]string) []string {
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	sort.Strings(result)
	return result
}

func envValue(env []string, key string) (string, bool) {
	for i := len(env) - 1; i >= 0; i-- {
		name, value, ok := strings.Cut(env[i], "=")
		if ok && (name == key || runtime.GOOS == "windows" && strings.EqualFold(name, key)) {
			return value, true
		}
	}
	return "", false
}

func allowRelativeExecutable() bool {
	settings := strings.Split(os.Getenv("GODEBUG"), ",")
	for i := len(settings) - 1; i >= 0; i-- {
		key, value, ok := strings.Cut(settings[i], "=")
		if ok && key == "execerrdot" {
			return value == "0"
		}
	}
	return false
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
