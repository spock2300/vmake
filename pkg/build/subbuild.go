package build

import (
	"fmt"
	"os"
	"os/exec"

	vlog "gitee.com/spock2300/vmake/pkg/log"
)

func SubBuild(tcName, dir string, extraArgs ...string) error {
	vmakeBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find vmake binary: %w", err)
	}

	args := append([]string{"build", "--toolchain", tcName}, extraArgs...)
	cmd := exec.Command(vmakeBin, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	vlog.Info("  [subbuild] %s (toolchain=%s)", dir, tcName)

	return cmd.Run()
}
