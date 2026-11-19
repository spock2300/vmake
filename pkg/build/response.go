package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	iexec "github.com/spock2300/vmake/internal/exec"
)

const safeInlineCommandUnits = 8192

type responseFile struct {
	path    string
	content string
}

func gnuRunner(env map[string]string) cmdRunner {
	return gnuRunnerContext(context.Background(), env)
}

func gnuRunnerContext(ctx context.Context, env map[string]string) cmdRunner {
	return func(name, dir string, args ...string) ([]byte, error) {
		return iexec.RunWithOptions(name, args, iexec.RunOptions{Dir: dir, Env: env, Context: ctx, CaptureStderr: true})
	}
}

func responseFileDir(hostOS, rspDir string) string {
	if hostOS != "windows" {
		return ""
	}
	if rspDir != "" {
		return rspDir
	}
	return filepath.Join(os.TempDir(), "vmake-rsp")
}

func formatResponseArgs(args []string) string {
	var data strings.Builder
	for _, arg := range args {
		data.WriteByte('"')
		data.WriteString(strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(arg))
		data.WriteString("\"\n")
	}
	return data.String()
}

func responseFileFor(dir string, args []string) *responseFile {
	content := formatResponseArgs(args)
	sum := sha256.Sum256([]byte(content))
	return &responseFile{
		path:    filepath.Join(dir, "rsp-"+digestDir(hex.EncodeToString(sum[:]))+".rsp"),
		content: content,
	}
}

func responseFileArg(hostOS, workDir string, file *responseFile) string {
	return "@" + commandPathFor(hostOS, workDir, file.path)
}

func ensureResponseFile(file *responseFile) error {
	if file == nil || file.path == "" {
		return nil
	}
	if info, err := os.Stat(file.path); err == nil && info.Size() == int64(len(file.content)) {
		return nil
	}
	dir := filepath.Dir(file.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create response file directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".rsp-*.tmp")
	if err != nil {
		return fmt.Errorf("create response file: %w", err)
	}
	tempName := temp.Name()
	_, writeErr := temp.WriteString(file.content)
	closeErr := temp.Close()
	if writeErr != nil {
		os.Remove(tempName)
		return fmt.Errorf("write response file: %w", writeErr)
	}
	if closeErr != nil {
		os.Remove(tempName)
		return fmt.Errorf("close response file: %w", closeErr)
	}
	if err := os.Rename(tempName, file.path); err != nil {
		os.Remove(tempName)
		if info, statErr := os.Stat(file.path); statErr == nil && info.Size() == int64(len(file.content)) {
			return nil
		}
		return fmt.Errorf("publish response file: %w", err)
	}
	return nil
}

func commandLineUnits(program string, args []string) int {
	units := utf16Units(program)
	for _, arg := range args {
		units += 1 + 2 + utf16Units(arg)
	}
	return units
}

func utf16Units(value string) int {
	units := 0
	for _, r := range value {
		if r > 0xFFFF {
			units += 2
		} else {
			units++
		}
	}
	return units
}
