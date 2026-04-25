package api

import (
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input   string
		want    Version
		wantOK  bool
	}{
		{"1.2.3", Version{1, 2, 3, ""}, true},
		{"v1.2.3", Version{1, 2, 3, ""}, true},
		{"1.2", Version{1, 2, 0, ""}, true},
		{"1", Version{1, 0, 0, ""}, true},
		{"v2.0.0-rc.1", Version{2, 0, 0, "rc.1"}, true},
		{"1.0.0-alpha", Version{1, 0, 0, "alpha"}, true},
		{"1.0.0-alpha.1", Version{1, 0, 0, "alpha.1"}, true},
		{"", Version{}, false},
		{"abc", Version{}, false},
	}

	for _, tt := range tests {
		got, ok := ParseVersion(tt.input)
		if ok != tt.wantOK {
			t.Errorf("ParseVersion(%q): ok = %v, want %v", tt.input, ok, tt.wantOK)
			continue
		}
		if ok && got != tt.want {
			t.Errorf("ParseVersion(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestVersionString(t *testing.T) {
	tests := []struct {
		v    Version
		want string
	}{
		{Version{1, 2, 3, ""}, "1.2.3"},
		{Version{2, 0, 0, "rc.1"}, "2.0.0-rc.1"},
		{Version{1, 0, 0, ""}, "1.0.0"},
	}
	for _, tt := range tests {
		got := tt.v.String()
		if got != tt.want {
			t.Errorf("Version(%v).String() = %q, want %q", tt.v, got, tt.want)
		}
	}
}

func sign(n int) int {
	if n < 0 {
		return -1
	}
	if n > 0 {
		return 1
	}
	return 0
}

func TestVersionCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.0.0", "1.0.0", 0},
		{"1.2.0", "1.3.0", -1},
		{"1.0.1", "1.0.2", -1},
		{"1.0.0-alpha", "1.0.0", -1},
		{"1.0.0", "1.0.0-alpha", 1},
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.2", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.1", 0},
		{"1.0.0-2", "1.0.0-10", -1},
		{"1.0.0-1", "1.0.0-alpha", -1},
		{"1.0.0-alpha", "1.0.0-1", 1},
	}

	for _, tt := range tests {
		a, _ := ParseVersion(tt.a)
		b, _ := ParseVersion(tt.b)
		got := sign(a.Compare(b))
		if got != tt.want {
			t.Errorf("Compare(%s, %s) = %d, want sign %d", tt.a, tt.b, a.Compare(b), tt.want)
		}
	}
}

func TestParseConstraint(t *testing.T) {
	tests := []struct {
		input string
		op    string
		major int
		minor int
		patch int
		ok    bool
	}{
		{">=1.2.3", ">=", 1, 2, 3, true},
		{">1.0.0", ">", 1, 0, 0, true},
		{"<=2.0.0", "<=", 2, 0, 0, true},
		{"<3.0", "<", 3, 0, 0, true},
		{"=1.2.3", "=", 1, 2, 3, true},
		{"~1.2.3", "~", 1, 2, 3, true},
		{"1.2.3", ">=", 1, 2, 3, true},
		{"", ">=", 0, 0, 0, true},
	}

	for _, tt := range tests {
		c, ok := ParseConstraint(tt.input)
		if ok != tt.ok {
			t.Errorf("ParseConstraint(%q): ok = %v, want %v", tt.input, ok, tt.ok)
			continue
		}
		if ok {
			if c.Op != tt.op {
				t.Errorf("ParseConstraint(%q).Op = %q, want %q", tt.input, c.Op, tt.op)
			}
			v := c.Version
			if v.Major != tt.major || v.Minor != tt.minor || v.Patch != tt.patch {
				t.Errorf("ParseConstraint(%q).Version = %v, want {%d,%d,%d}", tt.input, v, tt.major, tt.minor, tt.patch)
			}
		}
	}
}

func TestConstraintMatch(t *testing.T) {
	tests := []struct {
		constraint string
		version    string
		want       bool
	}{
		{">=1.0.0", "1.0.0", true},
		{">=1.0.0", "1.9.9", true},
		{">=1.0.0", "2.0.0", false},
		{">=1.0.0", "0.9.0", false},
		{">=2.0.0", "2.1.0", true},
		{">=2.0.0", "3.0.0", false},
		{">=0.0.0", "1.0.0", true},
		{">=0.0.0", "2.0.0", true},
		{">=0.0.0", "0.9.0", true},
		{">1.0.0", "1.0.0", false},
		{">1.0.0", "1.0.1", true},
		{">1.0.0", "2.0.0", true},
		{"<=2.0.0", "2.0.0", true},
		{"<=2.0.0", "2.0.1", false},
		{"<2.0.0", "1.9.9", true},
		{"<2.0.0", "2.0.0", false},
		{"=1.2.3", "1.2.3", true},
		{"=1.2.3", "1.2.4", false},
		{"~1.2.3", "1.2.3", true},
		{"~1.2.3", "1.2.9", true},
		{"~1.2.3", "1.3.0", false},
		{"~1.2.3", "1.1.9", false},
		{"", "0.0.1", true},
		{"", "99.99.99", true},
	}

	for _, tt := range tests {
		c, _ := ParseConstraint(tt.constraint)
		v, _ := ParseVersion(tt.version)
		got := c.Match(v)
		if got != tt.want {
			t.Errorf("Constraint(%q).Match(%s) = %v, want %v", tt.constraint, tt.version, got, tt.want)
		}
	}
}

func TestMatchVersion(t *testing.T) {
	available := []string{"1.0.0", "1.1.0", "1.2.0", "2.0.0", "2.1.0"}

	tests := []struct {
		constraint string
		want       string
		wantOK     bool
	}{
		{">=1.0.0", "1.2.0", true},
		{">=2.0.0", "2.1.0", true},
		{">=2.0.0 <2.1.0", "", false},
		{"~1.1.0", "1.1.0", true},
		{"=1.2.0", "1.2.0", true},
		{">=3.0.0", "", false},
		{"", "2.1.0", true},
	}

	for _, tt := range tests {
		got, ok := MatchVersion(available, tt.constraint)
		if ok != tt.wantOK {
			t.Errorf("MatchVersion(_, %q): ok = %v, want %v", tt.constraint, ok, tt.wantOK)
			continue
		}
		if ok && got != tt.want {
			t.Errorf("MatchVersion(_, %q) = %q, want %q", tt.constraint, got, tt.want)
		}
	}
}

func TestMatchVersionWithPreRelease(t *testing.T) {
	available := []string{"1.0.0-alpha", "1.0.0-beta", "1.0.0-rc.1", "1.0.0"}
	got, ok := MatchVersion(available, ">=1.0.0-alpha")
	if !ok || got != "1.0.0" {
		t.Errorf("MatchVersion pre-release: got %q ok=%v, want %q true", got, ok, "1.0.0")
	}
}

func TestCheckCycle(t *testing.T) {
	if err := CheckCycle([]string{"a", "b", "c"}, "b"); err == nil {
		t.Error("CheckCycle should detect cycle at b")
	}
	if err := CheckCycle([]string{"a", "b", "c"}, "d"); err != nil {
		t.Errorf("CheckCycle should not detect cycle: %v", err)
	}
}
