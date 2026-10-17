package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/spock2300/vmake/pkg/toolchain"
)

// BuildKey derives the per-package build directory key. extra carries
// additional key material: source version, commit hash, the global flags
// hash, the patch hash and the buildscript hash, joined with "\x00"
// separators.
func BuildKey(toolchain, mode string, options map[string]any, extra string) string {
	key, _ := BuildKeyE(toolchain, mode, options, extra)
	return key
}

func BuildKeyE(toolchain, mode string, options map[string]any, extra string) (string, error) {
	data := map[string]any{
		"format":     buildFormatVersion,
		"toolchain":  toolchain,
		"build_mode": mode,
		"options":    options,
	}
	if extra != "" {
		data["extra"] = extra
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("encode build key: %w", err)
	}

	h := sha256.Sum256(jsonData)
	return hex.EncodeToString(h[:]), nil
}

// GlobalFlagsHash summarizes the current global flags/links registered in the
// toolchain manager, so that flag changes invalidate build directories.
func GlobalFlagsHash() string {
	mgr := toolchain.GetManager()
	parts := [][]string{
		mgr.GetGlobalCFlags(),
		mgr.GetGlobalCxxFlags(),
		mgr.GetGlobalLdFlags(),
		mgr.GetGlobalLinks(),
	}
	h := sha256.New()
	for _, group := range parts {
		for _, s := range group {
			fmt.Fprintf(h, "%s\x00", s)
		}
		h.Write([]byte("\x01"))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// JoinKeyExtra combines version/commit/globalFlagsHash/patchHash/scriptHash
// into BuildKey extra material.
func JoinKeyExtra(version, commit, globalFlagsHash, patchHash, scriptHash string) string {
	return version + "\x00" + commit + "\x00" + globalFlagsHash + "\x00" + patchHash + "\x00" + scriptHash
}
