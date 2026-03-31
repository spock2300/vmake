package build

import (
	"encoding/base64"
	"encoding/json"
	"strings"
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

	key := base64.URLEncoding.EncodeToString(jsonData)
	return strings.TrimRight(key, "=")
}
