package buildreport

import (
	"bytes"
	"errors"
	"testing"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/pkg/build"
)

func TestFailureDetailsExtractsGNUDiagnosticsAndFormatsSummary(t *testing.T) {
	cmdErr := &iexec.CommandError{
		Output: "src/main.c:12:5: error: unknown type name 'foo'\n",
		Err:    errors.New("exit status 1"),
	}
	targetErr := &build.TargetError{
		Target: "app:main",
		Stage:  "compile",
		Err:    cmdErr,
	}
	diags, summary := FailureDetails(targetErr)
	if len(diags) != 1 {
		t.Fatalf("diagnostics = %+v, want 1", diags)
	}
	if diags[0].Code != "VMAKE_E_COMPILE" || diags[0].File != "src/main.c" || diags[0].Line != 12 || diags[0].Column != 5 {
		t.Fatalf("diag = %+v", diags[0])
	}
	if summary != "src/main.c:12:5: unknown type name 'foo'" {
		t.Fatalf("summary = %q", summary)
	}
}

func TestBeginFinishAndInvalidateReports(t *testing.T) {
	root := t.TempDir()
	r, err := Begin(root, CommandBuild, "")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	var runErr error
	r.Summary = &Summary{TargetsTotal: 1, TargetsUpToDate: 1, NoOp: true}
	r.Completed = true
	r.Finish(&runErr)
	if runErr != nil || r.Status != "succeeded" {
		t.Fatalf("Finish: err=%v, status=%s", runErr, r.Status)
	}
	var buf bytes.Buffer
	PrintSummary(&buf, r, nil)
	if buf.Len() == 0 {
		t.Fatal("PrintSummary produced empty output")
	}
	if err := InvalidateReports(root); err != nil {
		t.Fatalf("InvalidateReports: %v", err)
	}
}
