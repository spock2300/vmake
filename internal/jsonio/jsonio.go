package jsonio

import (
	"encoding/json"
	"os"

	"gitee.com/spock2300/vmake/internal/fs"
)

func Save(path string, v any) error {
	if err := fs.EnsureParentDir(path); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func Load(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
