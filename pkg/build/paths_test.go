package build

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestKeyDir(t *testing.T) {
	full := "761fa64b39dfa1018158230bb18f2784f5503d08d8b81918e33fb2614d1fb386"
	if got, want := KeyDir(full), "761fa64b39dfa101"; got != want {
		t.Errorf("KeyDir(%q) = %q, want %q", full, got, want)
	}
	for _, key := range []string{"abc", ""} {
		if got := KeyDir(key); got != key {
			t.Errorf("KeyDir(%q) = %q, want unchanged", key, got)
		}
	}
}

func TestMemberDir(t *testing.T) {
	cases := []struct{ member, want string }{
		{"", "root"},
		{"nested/member", "nested-member-5c350e13f056ef00"},
		{"a:b*c?", "a-b-c-7b7479d14aa89289"},
		{"-..-", "member-41d77d2f14e8e370"},
	}
	for _, c := range cases {
		if got := MemberDir(c.member); got != c.want {
			t.Errorf("MemberDir(%q) = %q, want %q", c.member, got, c.want)
		}
	}
	if MemberDir("Foo") == MemberDir("foo") {
		t.Error("member names differing only by case must stay distinct")
	}
	long := MemberDir(strings.Repeat("x", 200))
	if len(long) > 41 {
		t.Errorf("MemberDir(200 x) = %q, want a name of at most 41 characters", long)
	}
	if strings.HasPrefix(long, ".") || strings.HasSuffix(long, ".") || strings.HasSuffix(long, "-") {
		t.Errorf("MemberDir(200 x) = %q, want a portable name", long)
	}
}

func TestStateRecordNamesAreShort(t *testing.T) {
	name := "hl-mcu-sdk:system-freertos-kernel"
	for _, suffix := range []string{".link.json", ".void.json"} {
		record := pathKey(name) + suffix
		if len(record) != keyDirLen+len(suffix) {
			t.Errorf("state record %q is %d characters, want %d", record, len(record), keyDirLen+len(suffix))
		}
		if _, err := hex.DecodeString(record[:keyDirLen]); err != nil {
			t.Errorf("state record %q does not start with a hex digest: %v", record, err)
		}
	}
}

func TestDigestDir(t *testing.T) {
	full := "f6e399d8cb2ce9548432ebb53dafad4cb601e8a5204cd482288a7be0835c81d6"
	if got, want := digestDir(full), "f6e399d8cb2ce954"; got != want {
		t.Errorf("digestDir(%q) = %q, want %q", full, got, want)
	}
	if got := digestDir("abc"); got != "abc" {
		t.Errorf("digestDir(short) = %q, want unchanged", got)
	}
}
