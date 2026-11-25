package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	vlog "github.com/spock2300/vmake/pkg/log"
)

func TestRunIsRepeatableInOneProcess(t *testing.T) {
	var first, second bytes.Buffer
	var stderr1, stderr2 bytes.Buffer
	if code := run([]string{"version"}, &first, &stderr1, runEnv{}); code != 0 {
		t.Fatalf("first run exit code = %d", code)
	}
	if code := run([]string{"version"}, &second, &stderr2, runEnv{}); code != 0 {
		t.Fatalf("second run exit code = %d", code)
	}
	if second.Len() == 0 {
		t.Fatal("second run produced no output")
	}
}

func TestRunResetsFlagDefaultsBetweenInvocations(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"-q", "version"}, &out, &errOut, runEnv{}); code != 0 {
		t.Fatalf("quiet run exit code = %d", code)
	}
	if !quiet {
		t.Fatal("quiet flag was not set during the first run")
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"version"}, &out, &errOut, runEnv{}); code != 0 {
		t.Fatalf("second run exit code = %d", code)
	}
	if out.Len() == 0 {
		t.Fatal("second run produced no log output after a quiet run")
	}
	if quiet {
		t.Fatal("quiet flag kept its value from the previous invocation")
	}
	if vlog.GetLevel() != vlog.Normal {
		t.Fatalf("log level = %v, want Normal", vlog.GetLevel())
	}
}

func TestRunResetsProcessScopedState(t *testing.T) {
	cachedProjectDir = "/stale/project"
	activeCommandSession = &commandSession{}

	var out, errOut bytes.Buffer
	if code := run([]string{"version"}, &out, &errOut, runEnv{}); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if cachedProjectDir != "" {
		t.Fatalf("cachedProjectDir = %q, want empty", cachedProjectDir)
	}
	if activeCommandSession == nil || activeCommandSession.storage != nil {
		t.Fatalf("command session was not replaced and closed: %+v", activeCommandSession)
	}
}

func TestRunUsesEnvVmakeDir(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	if code := run([]string{"version"}, &out, &errOut, runEnv{VmakeDir: dir}); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if vmakeDir != dir {
		t.Fatalf("vmakeDir = %q, want %q", vmakeDir, dir)
	}

	out.Reset()
	if code := run([]string{"version"}, &out, &errOut, runEnv{}); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if vmakeDir != defaultVmakeDir {
		t.Fatalf("vmakeDir = %q, want the process default %q", vmakeDir, defaultVmakeDir)
	}
}

func TestRunReturnsUsageExitCode(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"version"}, &out, &errOut, runEnv{}); code != 0 {
		t.Fatalf("version exit code = %d, want 0", code)
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"build", "--no-such-flag"}, &out, &errOut, runEnv{}); code != 2 {
		t.Fatalf("unknown flag exit code = %d, want 2", code)
	}
	out.Reset()
	errOut.Reset()
	// An unknown command is a plain error, not a usage error: cobra reports it
	// after the flag/argument validators have already passed.
	if code := run([]string{"no-such-command"}, &out, &errOut, runEnv{}); code != 1 {
		t.Fatalf("unknown command exit code = %d, want 1", code)
	}
}

func TestFatalRunsDeferredCleanup(t *testing.T) {
	defer pipeStderr(io.Discard)()
	cleaned := false
	defer func() {
		value := recover()
		exit, ok := value.(*exitError)
		if !ok {
			t.Fatalf("panic value = %v, want *exitError", value)
		}
		if exit.VmakeExitCode() != 1 {
			t.Fatalf("exit code = %d, want 1", exit.VmakeExitCode())
		}
		if !cleaned {
			t.Fatal("deferred cleanup did not run before the fatal path unwound")
		}
	}()
	func() {
		defer func() { cleaned = true }()
		fatalErr(errors.New("boom"))
	}()
}

func TestFatalClassifiesUsageErrors(t *testing.T) {
	defer pipeStderr(io.Discard)()
	defer func() {
		value := recover()
		exit, ok := value.(*exitError)
		if !ok {
			t.Fatalf("panic value = %v, want *exitError", value)
		}
		if exit.VmakeExitCode() != 2 {
			t.Fatalf("exit code = %d, want 2", exit.VmakeExitCode())
		}
	}()
	fatalErr(&usageError{err: errors.New("bad usage")})
}

func TestCommandTreeSurvivesRebuild(t *testing.T) {
	var out, errOut bytes.Buffer
	run([]string{"version"}, &out, &errOut, runEnv{})
	run([]string{"version"}, &out, &errOut, runEnv{})

	if RootCmd.PersistentFlags().Lookup("project") == nil {
		t.Fatal("--project missing after a rebuild")
	}
	if _, _, err := RootCmd.Find([]string{"deps", "graph"}); err != nil {
		t.Fatalf("deps graph missing after a rebuild: %v", err)
	}
	if _, _, err := RootCmd.Find([]string{"inspect"}); err != nil {
		t.Fatalf("inspect missing after a rebuild: %v", err)
	}
	if got := generateCLIRef(RootCmd); !strings.Contains(got, "--project <value> -C") {
		t.Fatalf("CLI reference lost the --project entry:\n%s", got)
	}
}

func TestCommandReferenceStableAcrossRebuild(t *testing.T) {
	before := generateCLIRef(RootCmd)
	var out, errOut bytes.Buffer
	run([]string{"version"}, &out, &errOut, runEnv{})
	run([]string{"version"}, &out, &errOut, runEnv{})
	if after := generateCLIRef(RootCmd); after != before {
		t.Fatalf("command tree changed across a rebuild:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestUsageValidatorNotStackedAcrossInvocations(t *testing.T) {
	var out, errOut bytes.Buffer
	run([]string{"version"}, &out, &errOut, runEnv{})
	run([]string{"version"}, &out, &errOut, runEnv{})

	target := firstCommandWithArgs(RootCmd)
	if target == nil {
		t.Fatal("no command with an argument validator")
	}
	argErr := target.Args(target, []string{"unexpected", "extra"})
	if argErr == nil {
		argErr = target.Args(target, nil)
	}
	if argErr == nil {
		t.Fatalf("%s accepted both a wrong argument count and no arguments", target.CommandPath())
	}
	var usage *usageError
	if !errors.As(argErr, &usage) {
		t.Fatalf("validator error = %v, want a usage error", argErr)
	}
	var stacked *usageError
	if errors.As(usage.Unwrap(), &stacked) {
		t.Fatal("argument validator was wrapped more than once")
	}
}

func firstCommandWithArgs(cmd *cobra.Command) *cobra.Command {
	if cmd.Args != nil {
		return cmd
	}
	for _, child := range cmd.Commands() {
		if found := firstCommandWithArgs(child); found != nil {
			return found
		}
	}
	return nil
}
