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
