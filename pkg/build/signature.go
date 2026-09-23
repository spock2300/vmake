package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/spock2300/vmake/internal/jsonio"
)

const buildFormatVersion = 2

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
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[keyOf(key)] = value
		}
	}
	for key, value := range overrides {
		env[keyOf(key)] = value
	}
	return digestValue(env)
}

func fingerprintFiles(workDir string, paths []string) ([]fileState, error) {
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
		hash, err := FileHash(path)
		if err != nil {
			return nil, err
		}
		states = append(states, fileState{Path: path, Hash: hash, Mode: uint32(info.Mode())})
	}
	return states, nil
}

func actionUpToDate(recordPath, signature, workDir string, inputs, outputs []string) bool {
	var record actionRecord
	if err := jsonio.Load(recordPath, &record); err != nil || record.Version != buildFormatVersion || record.Signature != signature {
		return false
	}
	currentInputs, err := fingerprintFiles(workDir, inputs)
	if err != nil || !equalFileStates(record.Inputs, currentInputs) {
		return false
	}
	currentOutputs, err := fingerprintFiles(workDir, outputs)
	return err == nil && equalFileStates(record.Outputs, currentOutputs)
}

func equalFileStates(a, b []fileState) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func saveActionRecord(recordPath, signature, workDir string, inputs, outputs []string) error {
	inputStates, err := fingerprintFiles(workDir, inputs)
	if err != nil {
		return fmt.Errorf("record action inputs: %w", err)
	}
	outputStates, err := fingerprintFiles(workDir, outputs)
	if err != nil {
		return fmt.Errorf("record action outputs: %w", err)
	}
	return jsonio.Save(recordPath, actionRecord{Version: buildFormatVersion, Signature: signature, Inputs: inputStates, Outputs: outputStates})
}

func invalidateActionRecord(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
