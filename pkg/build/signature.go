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
	"github.com/spock2300/vmake/internal/jsonio"
)

// actionRecordVersion versions the on-disk action record and the signatures
// embedded in it. It is independent of layoutFormatVersion.
const actionRecordVersion = 2

type fileState struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
	Mode uint32 `json:"mode"`
}

type actionRecord struct {
	Version   int         `json:"version"`
	Signature string      `json:"signature"`
	Inputs    []fileState `json:"inputs"`
	Outputs   []fileState `json:"outputs"`
}

type commandSpec struct {
	Program string
	Args    []string
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
	return hex.EncodeToString(hash[:])
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
	if entry.info.Size() != info.Size() || entry.info.Mode() != info.Mode() || !entry.info.ModTime().Equal(info.ModTime()) || !os.SameFile(entry.info, info) {
		return false
	}
	if !entry.hasTime {
		return true
	}
	changed, hasTime := fileChangeTime(path, info)
	return hasTime && entry.changed.Equal(changed)
}

func fingerprintFiles(cache *fileHashCache, workDir string, paths []string) ([]fileState, error) {
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
	states := make([]fileState, 0, len(resolved))
	for _, path := range resolved {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("action input or output is not a regular file: %s", path)
		}
		hash, err := cache.hash(path, info)
		if err != nil {
			return nil, err
		}
		states = append(states, fileState{Path: path, Hash: hash, Mode: uint32(info.Mode())})
	}
	return states, nil
}

type actionDecision struct {
	UpToDate bool
	Reason   string
}

func actionUpToDate(cache *fileHashCache, recordPath, signature, workDir string, inputs, outputs []string) bool {
	return checkAction(cache, recordPath, signature, workDir, inputs, outputs).UpToDate
}

func checkAction(cache *fileHashCache, recordPath, signature, workDir string, inputs, outputs []string) actionDecision {
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
	currentInputs, err := fingerprintFiles(cache, workDir, inputs)
	if err != nil {
		return actionDecision{Reason: "input missing or unreadable"}
	}
	if !equalFileStates(record.Inputs, currentInputs) {
		return actionDecision{Reason: "inputs changed"}
	}
	currentOutputs, err := fingerprintFiles(cache, workDir, outputs)
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

func saveActionRecord(cache *fileHashCache, recordPath, signature, workDir string, inputs, outputs []string) error {
	inputStates, err := fingerprintFiles(cache, workDir, inputs)
	if err != nil {
		return fmt.Errorf("record action inputs: %w", err)
	}
	cache.evict(workDir, outputs)
	outputStates, err := fingerprintFiles(cache, workDir, outputs)
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
