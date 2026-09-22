package toolchain

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (m *Manager) RegisterRepo(repoDir, toolchainsDir string) {
	absoluteDir, err := filepath.Abs(repoDir)
	if err != nil {
		m.RegisterToolchainError("", repoDir, err)
		return
	}
	repoDir = absoluteDir
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		m.RegisterToolchainError("", repoDir, err)
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(repoDir, entry.Name(), "toolchain.json")
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			m.RegisterToolchainError("", path, err)
			continue
		}
		def, err := LoadToolchainDef(path)
		if err != nil {
			var defErr *DefinitionError
			name := ""
			if errors.As(err, &defErr) {
				name = defErr.Name()
			}
			m.RegisterToolchainError(name, path, err)
			continue
		}
		if previous := m.sources[def.Name]; previous != "" {
			if previous != path {
				m.RegisterToolchainError(def.Name, path, fmt.Errorf("duplicate name also defined in %s", previous))
			}
			continue
		}
		tc, err := def.ToToolchain(toolchainsDir)
		if err == nil {
			err = m.RegisterToolchain(def.Name, tc)
		}
		if err != nil {
			m.RegisterToolchainError(def.Name, path, err)
			continue
		}
		if m.sources == nil {
			m.sources = make(map[string]string)
		}
		m.sources[def.Name] = path
		if len(def.Installations) > 0 {
			m.SetOnMissing(def.Name, func(string) (*Toolchain, error) {
				return Install(*def, repoDir, toolchainsDir)
			})
		}
	}
}
