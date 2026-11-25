package pipeline

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestValidateTestExecutionRejectsCrossTargets(t *testing.T) {
	for _, platform := range []api.Platform{
		{OS: "none", Triple: "arm-none-eabi"},
		{OS: "linux", Triple: "aarch64-linux-gnu"},
		{OS: "windows"},
	} {
		if err := ValidateTestExecution(platform, "linux"); err == nil || !strings.Contains(err.Error(), "vmake build --tests") {
			t.Errorf("platform %v: %v", platform, err)
		}
	}
	if err := ValidateTestExecution(api.Platform{OS: "linux"}, "linux"); err != nil {
		t.Fatal(err)
	}
}

func TestBoundedTestOutputUTF8Boundary(t *testing.T) {
	keep := BuildTestOutputLimit - len(BuildTestOutputMarker)
	input := "head-" + "中" + strings.Repeat("b", keep-1)
	got := BoundedTestOutput(input)
	if len(got) > BuildTestOutputLimit {
		t.Fatalf("len(got) = %d, exceeds %d", len(got), BuildTestOutputLimit)
	}
	data, err := json.Marshal(struct {
		Output string `json:"output"`
	}{Output: got})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `\ufffd`) || strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("truncated output contains invalid UTF-8 replacement rune: %q", got)
	}
}
