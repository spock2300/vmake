package build

import (
	"fmt"
	"os"

	iexec "gitee.com/spock2300/vmake/internal/exec"
	vlog "gitee.com/spock2300/vmake/pkg/log"
)

func SubBuild(tcName, dir string, extraArgs ...string) error {
	vmakeBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find vmake binary: %w", err)
	}

	args := append([]string{"build", "--toolchain", tcName}, extraArgs...)

	vlog.Info("  [subbuild] %s (toolchain=%s)", dir, tcName)

	return iexec.RunToStdout(dir, vmakeBin, args...)
}
