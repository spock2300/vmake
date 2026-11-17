package build

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// keyDirLen is the directory-name projection of a digest: 64 bits keep the
// output trees of one project distinct while leaving the rest of the Windows
// path budget to the files generated inside them.
const keyDirLen = 16

// KeyDir projects a build key to the name of its output directory. The full
// key remains the identity; the directory name only has to be unique under one
// out/ root, so it does not spend 64 hex characters of a path.
func KeyDir(key string) string {
	return digestDir(key)
}

// digestDir projects a hex digest to one path component.
func digestDir(digest string) string {
	if len(digest) <= keyDirLen {
		return digest
	}
	return digest[:keyDirLen]
}

// MemberDir projects a package member path to one portable directory name.
// Path separators and characters Windows rejects become '-', the readable part
// is capped, and the 16 hex suffix keeps members that sanitize alike distinct.
func MemberDir(member string) string {
	if member == "" {
		return "root"
	}
	var b strings.Builder
	for _, r := range member {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), ".-")
	if len(name) > 24 {
		name = strings.Trim(name[:24], ".-")
	}
	if name == "" {
		name = "member"
	}
	sum := sha256.Sum256([]byte(member))
	return name + "-" + hex.EncodeToString(sum[:])[:16]
}

// BuildPath joins subdir inside the build key directory under baseDir.
func BuildPath(baseDir, buildKey, subdir string) string {
	return filepath.Join(baseDir, "build", KeyDir(buildKey), subdir)
}
