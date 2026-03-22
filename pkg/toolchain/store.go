package toolchain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gitee.com/spock2300/vmake/internal/jsonio"
)

const GlobalConfigVersion = "1"

func GetGlobalConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "~"
	}
	return filepath.Join(home, ".vmake", "config.json")
}

func LoadGlobal() (*GlobalConfig, error) {
	path := GetGlobalConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return GetBuiltinDefault(), nil
		}
		return nil, fmt.Errorf("failed to read global config: %w", err)
	}

	var cfg GlobalConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse global config: %w", err)
	}

	if cfg.Toolchains == nil {
		cfg.Toolchains = make(map[string]*Toolchain)
	}

	return &cfg, nil
}

func SaveGlobal(cfg *GlobalConfig) error {
	return jsonio.Save(GetGlobalConfigPath(), cfg)
}

func GetBuiltinDefault() *GlobalConfig {
	return &GlobalConfig{
		Version:          GlobalConfigVersion,
		DefaultToolchain: "gcc",
		Toolchains: map[string]*Toolchain{
			"gcc": {
				Name:        "gcc",
				DisplayName: "System GCC",
				Host:        "x86_64-linux-gnu",
				Tools: Tools{
					CC:     "gcc",
					CXX:    "g++",
					AR:     "ar",
					LD:     "ld",
					STRIP:  "strip",
					RANLIB: "ranlib",
				},
				DefaultFlags: DefaultFlags{
					CFlags:   []string{"-O2", "-Wall", "-Wstrict-prototypes", "-fno-strict-aliasing", "-fno-common", "-fPIC"},
					CxxFlags: []string{"-O2", "-Wall", "-Wextra", "-fno-strict-aliasing", "-fno-common", "-fPIC"},
					LdFlags:  []string{"-Wl,--as-needed"},
				},
			},
		},
	}
}

func GetDefaultTemplate() *GlobalConfig {
	return &GlobalConfig{
		Version:          GlobalConfigVersion,
		DefaultToolchain: "gcc",
		Toolchains: map[string]*Toolchain{
			"gcc": {
				Name:        "gcc",
				DisplayName: "System GCC",
				Host:        "x86_64-linux-gnu",
				Tools: Tools{
					CC:     "gcc",
					CXX:    "g++",
					AR:     "ar",
					LD:     "ld",
					STRIP:  "strip",
					RANLIB: "ranlib",
				},
				DefaultFlags: DefaultFlags{
					CFlags:   []string{"-O2", "-Wall", "-Wstrict-prototypes", "-fno-strict-aliasing", "-fno-common", "-fPIC"},
					CxxFlags: []string{"-O2", "-Wall", "-Wextra", "-fno-strict-aliasing", "-fno-common", "-fPIC"},
					LdFlags:  []string{"-Wl,--as-needed"},
				},
				DownloadURL: "",
				InstallPath: "",
			},
			"clang": {
				Name:        "clang",
				DisplayName: "Clang",
				Host:        "x86_64-linux-gnu",
				Tools: Tools{
					CC:     "clang",
					CXX:    "clang++",
					AR:     "llvm-ar",
					LD:     "lld",
					STRIP:  "llvm-strip",
					RANLIB: "llvm-ranlib",
				},
				DefaultFlags: DefaultFlags{
					CFlags:   []string{"-O2", "-Wall", "-Wstrict-prototypes", "-Wno-gnu-variable-sized-type-not-at-end"},
					CxxFlags: []string{"-O2", "-Wall", "-Wextra", "-Wno-gnu-variable-sized-type-not-at-end"},
					LdFlags:  []string{"-Wl,--as-needed"},
				},
				DownloadURL: "",
				InstallPath: "",
			},
			"arm-gcc": {
				Name:        "arm-gcc",
				DisplayName: "ARM Cross GCC",
				Host:        "arm-linux-gnueabihf",
				Tools: Tools{
					CC:     "arm-linux-gnueabihf-gcc",
					CXX:    "arm-linux-gnueabihf-g++",
					AR:     "arm-linux-gnueabihf-ar",
					LD:     "arm-linux-gnueabihf-ld",
					STRIP:  "arm-linux-gnueabihf-strip",
					RANLIB: "arm-linux-gnueabihf-ranlib",
				},
				DefaultFlags: DefaultFlags{
					CFlags:   []string{"-O2", "-Wall", "-Wstrict-prototypes", "-fno-strict-aliasing", "-fno-common", "-fno-pic", "-march=armv7-a", "-mfpu=neon"},
					CxxFlags: []string{"-O2", "-Wall", "-Wextra", "-fno-strict-aliasing", "-fno-common", "-fno-pic", "-march=armv7-a", "-mfpu=neon"},
					LdFlags:  []string{"-Wl,--as-needed"},
				},
				DownloadURL: "https://example.com/arm-toolchain.tar.gz",
				InstallPath: "/opt/arm-toolchain",
			},
		},
	}
}

func ExistsGlobalConfig() bool {
	path := GetGlobalConfigPath()
	_, err := os.Stat(path)
	return err == nil
}
