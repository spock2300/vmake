package toolchain

type Toolchain struct {
	Name         string       `json:"name"`
	DisplayName  string       `json:"display_name"`
	Host         string       `json:"host"`
	Prefix       string       `json:"prefix"`
	Tools        Tools        `json:"tools"`
	DefaultFlags DefaultFlags `json:"default_flags"`
	InstallPath  string       `json:"install_path"`
}

type Tools struct {
	CC     string `json:"cc"`
	CXX    string `json:"cxx"`
	AR     string `json:"ar"`
	LD     string `json:"ld"`
	STRIP  string `json:"strip"`
	RANLIB string `json:"ranlib"`
}

type DefaultFlags struct {
	CFlags   []string `json:"cflags"`
	CxxFlags []string `json:"cxxflags"`
	LdFlags  []string `json:"ldflags"`
}
