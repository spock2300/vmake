package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func BuildKey(toolchain, mode string, options map[string]any) string {
	data := map[string]any{
		"toolchain":  toolchain,
		"build_mode": mode,
		"options":    options,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return ""
	}

	h := sha256.Sum256(jsonData)
	return hex.EncodeToString(h[:])
}
