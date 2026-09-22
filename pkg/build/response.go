package build

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	iexec "github.com/spock2300/vmake/internal/exec"
)

func runGNU(name, dir string, args ...string) ([]byte, error) {
	if runtime.GOOS != "windows" {
		return iexec.RunInDir(name, dir, args...)
	}
	return runGNUResponse(name, dir, args, iexec.RunInDir)
}

func runGNUResponse(name, dir string, args []string, run cmdRunner) ([]byte, error) {
	output := ""
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-o" {
			output = args[i+1]
			break
		}
	}
	if len(args) >= 2 && args[0] == "rcs" {
		output = args[1]
	}
	if output == "" {
		return nil, fmt.Errorf("%s: no output path for GNU response file", name)
	}
	f, err := os.CreateTemp(filepath.Dir(resolveWorkPath(dir, output)), "vmake-*.rsp")
	if err != nil {
		return nil, fmt.Errorf("create response file: %w", err)
	}
	defer os.Remove(f.Name())
	var data strings.Builder
	for _, arg := range args {
		data.WriteByte('"')
		data.WriteString(strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(arg))
		data.WriteString("\"\n")
	}
	_, writeErr := f.WriteString(data.String())
	closeErr := f.Close()
	if writeErr != nil {
		return nil, fmt.Errorf("write response file: %w", writeErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close response file: %w", closeErr)
	}
	out, err := run(name, dir, "@"+commandPath(dir, f.Name()))
	if err != nil {
		return out, fmt.Errorf("%s: %w", iexec.FormatCommandLine(name, args), err)
	}
	return out, nil
}
