package api

type Toolchain struct {
	Target   string
	CC       string
	CXX      string
	LD       string
	AR       string
	CFlags   string
	CXXFlags string
	LDFlags  string
	SysRoot  string
}

func NewToolchain() *Toolchain {
	return &Toolchain{}
}

func (t *Toolchain) SetTarget(target string) *Toolchain {
	t.Target = target
	return t
}

func (t *Toolchain) SetCC(cc string) *Toolchain {
	t.CC = cc
	return t
}

func (t *Toolchain) SetCXX(cxx string) *Toolchain {
	t.CXX = cxx
	return t
}

func (t *Toolchain) SetLD(ld string) *Toolchain {
	t.LD = ld
	return t
}

func (t *Toolchain) SetAR(ar string) *Toolchain {
	t.AR = ar
	return t
}

func (t *Toolchain) SetCFlags(flags string) *Toolchain {
	t.CFlags = flags
	return t
}

func (t *Toolchain) SetCXXFlags(flags string) *Toolchain {
	t.CXXFlags = flags
	return t
}

func (t *Toolchain) SetLDFlags(flags string) *Toolchain {
	t.LDFlags = flags
	return t
}

func (t *Toolchain) SetSysRoot(sysroot string) *Toolchain {
	t.SysRoot = sysroot
	return t
}

func (t *Toolchain) Env() map[string]string {
	env := map[string]string{
		"CC":       t.CC,
		"CXX":      t.CXX,
		"LD":       t.LD,
		"AR":       t.AR,
		"CFLAGS":   t.CFlags,
		"CXXFLAGS": t.CXXFlags,
		"LDFLAGS":  t.LDFlags,
	}
	if t.SysRoot != "" {
		env["SYSROOT"] = t.SysRoot
	}
	return env
}
