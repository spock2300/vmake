package jsonio

import (
	"encoding/json"
	"os"

	"github.com/spock2300/vmake/internal/fs"
)

func Save(path string, v any) error {
	if err := fs.EnsureParentDir(path); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return fs.RenameRetry(tmp, path)
}

func Load(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
