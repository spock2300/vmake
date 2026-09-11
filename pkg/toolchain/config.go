package toolchain

import (
	"runtime"
	"strings"
)

type Toolchain struct {
	Name         string       `json:"name"`
	DisplayName  string       `json:"display_name"`
	Host         string       `json:"host"`
	Prefix       string       `json:"prefix"`
	TargetOS     string       `json:"target_os"`
	Tools        Tools        `json:"tools"`
	DefaultFlags DefaultFlags `json:"default_flags"`
	InstallPath  string       `json:"install_path"`
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

// TargetOSOrDefault returns the toolchain's target OS. An unset target OS
// means "same as the host".
func (t *Toolchain) TargetOSOrDefault() string {
	if t.TargetOS != "" {
		return t.TargetOS
	}
	return runtime.GOOS
}

// TargetOSOf returns tc's target OS, defaulting to the host when tc is nil.
func TargetOSOf(tc *Toolchain) string {
	if tc == nil {
		return runtime.GOOS
	}
	return tc.TargetOSOrDefault()
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

type DefaultFlags struct {
	CFlags   []string `json:"cflags"`
	CxxFlags []string `json:"cxxflags"`
	LdFlags  []string `json:"ldflags"`
}

func (t *Toolchain) Env() map[string]string {
	env := map[string]string{
		"CC":       t.Tools.CC,
		"CXX":      t.Tools.CXX,
		"LD":       t.Tools.LD,
		"AR":       t.Tools.AR,
		"CFLAGS":   strings.Join(t.DefaultFlags.CFlags, " "),
		"CXXFLAGS": strings.Join(t.DefaultFlags.CxxFlags, " "),
		"LDFLAGS":  strings.Join(t.DefaultFlags.LdFlags, " "),
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
