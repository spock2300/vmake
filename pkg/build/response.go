package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/flock"
	"github.com/spock2300/vmake/internal/fs"
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

// ready checks the file through a read handle, as the compiler will open it.
func (file *responseFile) ready() (bool, error) {
	f, err := os.Open(file.path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("response file is not a regular file: %s", file.path)
	}
	return info.Size() == int64(len(file.content)), nil
}

type responseDirGate struct {
	ch   chan struct{}
	refs int
}

var (
	responseDirMu    sync.Mutex
	responseDirGates = make(map[string]*responseDirGate)
)

func responseDirGateKey(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	key := filepath.Clean(dir)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return key
}

func acquireResponseDirGate(ctx context.Context, dir string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := responseDirGateKey(dir)
	responseDirMu.Lock()
	gate := responseDirGates[key]
	if gate == nil {
		gate = &responseDirGate{ch: make(chan struct{}, 1)}
		responseDirGates[key] = gate
	}
	gate.refs++
	responseDirMu.Unlock()

	releaseRef := func() {
		responseDirMu.Lock()
		gate.refs--
		if gate.refs == 0 {
			delete(responseDirGates, key)
		}
		responseDirMu.Unlock()
	}

	select {
	case gate.ch <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-gate.ch
			releaseRef()
			return nil, err
		}
		return func() {
			<-gate.ch
			releaseRef()
		}, nil
	case <-ctx.Done():
		releaseRef()
		return nil, ctx.Err()
	}
}

func ensureResponseFile(ctx context.Context, file *responseFile) (err error) {
	if file == nil || file.path == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ready, _ := file.ready(); ready {
		return nil
	}
	dir := filepath.Dir(file.path)
	releaseGate, err := acquireResponseDirGate(ctx, dir)
	if err != nil {
		return fmt.Errorf("lock response file publication: %w", err)
	}
	defer releaseGate()
	if ready, _ := file.ready(); ready {
		return nil
	}
	// A later rename of an already published response file can deny GCC its
	// first read on Windows. The fallback temp cache can span processes.
	lock, err := flock.AcquireContext(ctx, dir+".lock")
	if err != nil {
		return fmt.Errorf("lock response file publication: %w", err)
	}
	defer func() { err = errors.Join(err, lock.Release()) }()
	if ready, err := file.ready(); err != nil || ready {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create response file directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".rsp-*.tmp")
	if err != nil {
		return fmt.Errorf("create response file: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	_, writeErr := temp.WriteString(file.content)
	closeErr := temp.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("write response file: %w", errors.Join(writeErr, closeErr))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := fs.RenameRetry(tempName, file.path); err != nil {
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
