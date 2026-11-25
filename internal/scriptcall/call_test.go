package scriptcall

import (
	"strings"
	"testing"
)

type nilError struct{}

func (*nilError) Error() string { return "nil error" }

func TestRecoverTreatsTypedNilErrorAsFailure(t *testing.T) {
	var err error
	func() {
		defer Recover(&err)
		panic((*nilError)(nil))
	}()
	if err == nil {
		t.Fatal("typed-nil panic recovered as success")
	}
	if !strings.Contains(err.Error(), "script panic") {
		t.Fatalf("error = %v, want a formatted panic", err)
	}
}

func TestRecoverPreservesErrorValues(t *testing.T) {
	want := &nilError{}
	var err error
	func() {
		defer Recover(&err)
		panic(want)
	}()
	if err != want {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

type exitSignalPanic struct{ code int }

func (e exitSignalPanic) Error() string      { return "exit" }
func (e exitSignalPanic) VmakeExitCode() int { return e.code }

func TestRecoverReraisesExitSignal(t *testing.T) {
	defer func() {
		value := recover()
		signal, ok := value.(exitSignalPanic)
		if !ok {
			t.Fatalf("panic value = %v, want the original exit signal", value)
		}
		if signal.VmakeExitCode() != 2 {
			t.Fatalf("exit code = %d, want 2", signal.VmakeExitCode())
		}
	}()
	var err error
	func() {
		defer Recover(&err)
		panic(exitSignalPanic{code: 2})
	}()
	t.Fatalf("exit signal was swallowed into %v", err)
}
