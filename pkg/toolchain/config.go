package toolchain

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Toolchain struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Prefix      string `json:"prefix"`
	Tools       Tools  `json:"tools"`
	InstallPath string `json:"install_path"`
}

type Tools struct {
	CC      string `json:"cc"`
	CXX     string `json:"cxx"`
	AR      string `json:"ar"`
	LD      string `json:"ld"`
	STRIP   string `json:"strip"`
	RANLIB  string `json:"ranlib"`
	OBJCOPY string `json:"objcopy"`
	SIZE    string `json:"size"`
	OBJDUMP string `json:"objdump"`
	NM      string `json:"nm"`
	MAKE    string `json:"make"`
}

// MakeTool returns the make program to invoke.
func (t *Toolchain) MakeTool() string {
	if t.Tools.MAKE != "" {
		return t.Tools.MAKE
	}
	return "make"
}

// MakeToolOf returns tc's make program, defaulting to "make" when tc is nil.
// Script-facing helpers may run before a toolchain is wired up.
func MakeToolOf(tc *Toolchain) string {
	if tc == nil {
		return "make"
	}
	return tc.MakeTool()
}

func (t *Toolchain) Env() map[string]string {
	env := map[string]string{
		"CC":  t.Tools.CC,
		"CXX": t.Tools.CXX,
		"LD":  t.Tools.LD,
		"AR":  t.Tools.AR,
	}
	if t.Prefix != "" {
		env["CROSS_COMPILE"] = t.Prefix
	}
	if t.Tools.OBJCOPY != "" {
		env["OBJCOPY"] = t.Tools.OBJCOPY
	}
	if t.Tools.SIZE != "" {
		env["SIZE"] = t.Tools.SIZE
	}
	if t.Tools.OBJDUMP != "" {
		env["OBJDUMP"] = t.Tools.OBJDUMP
	}
	if t.Tools.NM != "" {
		env["NM"] = t.Tools.NM
	}
	env["MAKE"] = t.MakeTool()
	return env
}

func (t *Toolchain) CommandEnv() map[string]string {
	env := make(map[string]string)
	if t == nil {
		return env
	}
	var dirs []string
	addDir := func(dir string) {
		for _, existing := range dirs {
			if existing == dir || runtime.GOOS == "windows" && strings.EqualFold(existing, dir) {
				return
			}
		}
		dirs = append(dirs, dir)
	}
	if t.InstallPath != "" {
		addDir(filepath.Join(t.InstallPath, "bin"))
	}
	for _, compiler := range []string{t.Tools.CC, t.Tools.CXX} {
		if filepath.IsAbs(compiler) {
			addDir(filepath.Dir(compiler))
		}
	}
	if len(dirs) > 0 {
		path := strings.Join(dirs, string(os.PathListSeparator))
		if inherited := os.Getenv("PATH"); inherited != "" {
			path += string(os.PathListSeparator) + inherited
		}
		env["PATH"] = path
	}
	return env
}
