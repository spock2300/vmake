package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spock2300/vmake/internal/envsig"
	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/jsonio"
)

// actionRecordVersion versions the on-disk action record and the signatures
// embedded in it. It is independent of layoutFormatVersion.
const actionRecordVersion = 4

// fileState records a file's content hash or stat identity.
type fileState struct {
	Path    string `json:"path"`
	Hash    string `json:"hash,omitempty"`
	Size    int64  `json:"size,omitempty"`
	ModTime int64  `json:"mtime,omitempty"`
	Mode    uint32 `json:"mode"`
}

type actionRecord struct {
	Version   int         `json:"version"`
	Signature string      `json:"signature"`
	Inputs    []fileState `json:"inputs"`
	Outputs   []fileState `json:"outputs"`
}

// actionFiles are the paths one incremental action checks. StatInputs is the
// build-product subset of Inputs and is compared by stat identity.
type actionFiles struct {
	Inputs     []string
	StatInputs []string
	Outputs    []string
}

type commandSpec struct {
	Program  string
	Args     []string
	response *responseFile
}

func digestValue(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

func pathKey(path string) string {
	hash := sha256.Sum256([]byte(filepath.ToSlash(filepath.Clean(path))))
	return digestDir(hex.EncodeToString(hash[:]))
}

func environmentSignature(overrides map[string]string) (string, error) {
	env := make(map[string]string)
	keyOf := func(key string) string {
		if runtime.GOOS == "windows" {
			return strings.ToUpper(key)
		}
		return key
	}
	// Only variables that can affect compiler, linker or CMake output are
	// fingerprinted. Hashing the whole process environment made unrelated
	// changes (MAKEFLAGS, TERM, PWD, VMAKE_*) discard every action record.
	// VMAKE_ENV_EXTRA is an additive escape hatch for unusual toolchains.
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok && envsig.Allowed(keyOf(key)) {
			env[keyOf(key)] = value
		}
	}
	// Toolchain-provided overrides are explicit inputs even when they use a key
	// outside the allowlist.
	for key, value := range overrides {
		env[keyOf(key)] = value
	}
	return digestValue(env)
}

type fileHashCache struct {
	mu     sync.Mutex
	hashes map[string]fileHashEntry
}

type fileHashEntry struct {
	info    os.FileInfo
	changed time.Time
	hasTime bool
	hash    string
}

func newFileHashCache() *fileHashCache {
	return &fileHashCache{hashes: make(map[string]fileHashEntry)}
}

func (c *fileHashCache) hash(path string, info os.FileInfo) (string, error) {
	if c == nil {
		return FileHash(path)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.hashes[path]; ok && sameFileIdentity(entry, path, info) {
		return entry.hash, nil
	}
	hash, err := FileHash(path)
	if err != nil {
		return "", err
	}
	changed, hasTime := fileChangeTime(path, info)
	c.hashes[path] = fileHashEntry{info: info, changed: changed, hasTime: hasTime, hash: hash}
	return hash, nil
}

func sameFileIdentity(entry fileHashEntry, path string, info os.FileInfo) bool {
	if entry.info.Mode() != info.Mode() || !fs.SameStat(entry.info, info) {
		return false
	}
	if !entry.hasTime {
		return true
	}
	changed, hasTime := fileChangeTime(path, info)
	return hasTime && entry.changed.Equal(changed)
}

func fingerprintInputs(cache *fileHashCache, workDir string, files actionFiles) ([]fileState, error) {
	statSet := make(map[string]bool, len(files.StatInputs))
	for _, path := range files.StatInputs {
		if path == "" {
			continue
		}
		absolute, err := filepath.Abs(resolveWorkPath(workDir, path))
		if err != nil {
			continue
		}
		statSet[filepath.Clean(absolute)] = true
	}
	resolved, err := resolveFingerprintPaths(workDir, files.Inputs)
	if err != nil {
		return nil, err
	}
	states := make([]fileState, 0, len(resolved))
	for _, path := range resolved {
		info, err := statRegularFile(path)
		if err != nil {
			return nil, err
		}
		state := fileState{Path: path, Mode: uint32(info.Mode())}
		if statSet[path] {
			state.Size = info.Size()
			state.ModTime = info.ModTime().UnixNano()
		} else {
			state.Hash, err = cache.hash(path, info)
			if err != nil {
				return nil, err
			}
		}
		states = append(states, state)
	}
	return states, nil
}

func fingerprintOutputs(workDir string, outputs []string) ([]fileState, error) {
	resolved, err := resolveFingerprintPaths(workDir, outputs)
	if err != nil {
		return nil, err
	}
	states := make([]fileState, 0, len(resolved))
	for _, path := range resolved {
		info, err := statRegularFile(path)
		if err != nil {
			return nil, err
		}
		states = append(states, fileState{
			Path:    path,
			Size:    info.Size(),
			ModTime: info.ModTime().UnixNano(),
			Mode:    uint32(info.Mode()),
		})
	}
	return states, nil
}

func resolveFingerprintPaths(workDir string, paths []string) ([]string, error) {
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		absolute, err := filepath.Abs(resolveWorkPath(workDir, path))
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, filepath.Clean(absolute))
	}
	resolved = unique(resolved)
	sort.Strings(resolved)
	return resolved, nil
}

func statRegularFile(path string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("action input or output is not a regular file: %s", path)
	}
	return info, nil
}

type actionDecision struct {
	UpToDate bool
	Reason   string
}

func checkAction(cache *fileHashCache, recordPath, signature, workDir string, files actionFiles) actionDecision {
	var record actionRecord
	if err := jsonio.Load(recordPath, &record); err != nil {
		if os.IsNotExist(err) {
			return actionDecision{Reason: "no action record"}
		}
		return actionDecision{Reason: "unreadable action record"}
	}
	if record.Version != actionRecordVersion {
		return actionDecision{Reason: "action record version changed"}
	}
	if record.Signature != signature {
		return actionDecision{Reason: "command, tool or environment changed"}
	}
	currentInputs, err := fingerprintInputs(cache, workDir, files)
	if err != nil {
		return actionDecision{Reason: "input missing or unreadable"}
	}
	if !equalFileStates(record.Inputs, currentInputs) {
		return actionDecision{Reason: "inputs changed"}
	}
	currentOutputs, err := fingerprintOutputs(workDir, files.Outputs)
	if err != nil {
		return actionDecision{Reason: "output missing or unreadable"}
	}
	if !equalFileStates(record.Outputs, currentOutputs) {
		return actionDecision{Reason: "outputs changed"}
	}
	return actionDecision{UpToDate: true}
}

func equalFileStates(a, b []fileState) bool {
	return slices.Equal(a, b)
}

func (c *fileHashCache) evict(workDir string, paths []string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, path := range paths {
		if path == "" {
			continue
		}
		if absolute, err := filepath.Abs(resolveWorkPath(workDir, path)); err == nil {
			delete(c.hashes, filepath.Clean(absolute))
		}
	}
}

func saveActionRecord(cache *fileHashCache, recordPath, signature, workDir string, files actionFiles) error {
	inputStates, err := fingerprintInputs(cache, workDir, files)
	if err != nil {
		return fmt.Errorf("record action inputs: %w", err)
	}
	cache.evict(workDir, files.Outputs)
	outputStates, err := fingerprintOutputs(workDir, files.Outputs)
	if err != nil {
		return fmt.Errorf("record action outputs: %w", err)
	}
	return jsonio.Save(recordPath, actionRecord{Version: actionRecordVersion, Signature: signature, Inputs: inputStates, Outputs: outputStates})
}

func invalidateActionRecord(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
