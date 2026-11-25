package api

import (
	"fmt"
	"strings"
)

const LocalContainerID = "//"

func IsLocalPackageID(id string) bool {
	return id == LocalContainerID || strings.HasPrefix(id, LocalContainerID)
}

func LocalRelPath(id string) (string, bool) {
	if id == LocalContainerID {
		return "", true
	}
	if strings.HasPrefix(id, LocalContainerID) {
		return id[len(LocalContainerID):], true
	}
	return "", false
}

func CanonicalizePackageRef(callerContainer, rawRef string) string {
	if rawRef == "" {
		return ""
	}
	if rawRef == LocalContainerID {
		if callerContainer == "" || callerContainer == LocalContainerID {
			return LocalContainerID
		}
		return callerContainer
	}
	if strings.HasPrefix(rawRef, LocalContainerID) {
		rel := strings.TrimPrefix(rawRef, LocalContainerID)
		if callerContainer == "" || callerContainer == LocalContainerID {
			return LocalContainerID + rel
		}
		return callerContainer + "/" + rel
	}
	return rawRef
}

func ValidatePackageRef(ref string) error {
	if ref == "" {
		return fmt.Errorf("empty package reference")
	}
	if strings.ContainsAny(ref, " \t\r\n") {
		return fmt.Errorf("invalid package reference %q: whitespace not allowed (version constraints belong in vmake.json)", ref)
	}
	if strings.ContainsAny(ref, "><=~@") {
		return fmt.Errorf("invalid package reference %q: version constraints belong in vmake.json, not build.go", ref)
	}
	if strings.Contains(ref, ":") {
		return fmt.Errorf("invalid package reference %q: ':' is not allowed in package IDs", ref)
	}
	if ref == LocalContainerID {
		return nil
	}
	if strings.HasPrefix(ref, LocalContainerID) {
		return validatePathSegments(ref, ref[len(LocalContainerID):])
	}
	if !strings.Contains(ref, "/") {
		return fmt.Errorf("invalid package reference %q: local packages must start with '//' and external packages must include '<repo>/<module>'", ref)
	}
	return validatePathSegments(ref, ref)
}

func ValidateContainerID(id string) error {
	if id == "" {
		return fmt.Errorf("empty container ID")
	}
	if id == LocalContainerID {
		return nil
	}
	if strings.ContainsAny(id, " \t\r\n><=~@:") {
		return fmt.Errorf("invalid container ID %q", id)
	}
	return validatePathSegments(id, id)
}

func validatePathSegments(raw, path string) error {
	if path == "" || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") || strings.Contains(path, "//") {
		return fmt.Errorf("invalid package reference %q: malformed path", raw)
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("invalid package reference %q: invalid path segment %q", raw, seg)
		}
	}
	return nil
}

func SplitPackageRef(ref string) (repo, name string, ok bool) {
	if IsLocalPackageID(ref) {
		return "", ref, false
	}
	idx := strings.Index(ref, "/")
	if idx < 0 {
		return "", ref, false
	}
	return ref[:idx], ref[idx+1:], true
}
