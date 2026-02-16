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

func ParseCacheHash(hash string) (toolchain, mode string, options map[string]any, err error) {
	if pad := len(hash) % 4; pad != 0 {
		hash += strings.Repeat("=", 4-pad)
	}

	jsonData, err := base64.URLEncoding.DecodeString(hash)
	if err != nil {
		return "", "", nil, err
	}

	var data map[string]any
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return "", "", nil, err
	}

	if tc, ok := data["toolchain"].(string); ok {
		toolchain = tc
	}
	if m, ok := data["mode"].(string); ok {
		mode = m
	}
	if opts, ok := data["options"].(map[string]any); ok {
		options = opts
	}

	return toolchain, mode, options, nil
}
