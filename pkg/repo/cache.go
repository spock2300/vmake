package repo

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

func CacheHash(toolchain, mode string, options map[string]any) string {
	data := map[string]any{
		"toolchain": toolchain,
		"mode":      mode,
		"options":   options,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return ""
	}

	hash := base64.URLEncoding.EncodeToString(jsonData)
	hash = strings.TrimRight(hash, "=")
	return hash
}
