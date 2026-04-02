package api

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Version struct {
	Major int
	Minor int
	Patch int
	Pre   string
}

type Constraint struct {
	Op      string
	Version Version
}

func ParseVersion(s string) (Version, bool) {
	re := regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:-(.+))?$`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return Version{}, false
	}

	v := Version{}
	v.Major, _ = strconv.Atoi(matches[1])
	if matches[2] != "" {
		v.Minor, _ = strconv.Atoi(matches[2])
	}
	if matches[3] != "" {
		v.Patch, _ = strconv.Atoi(matches[3])
	}
	if len(matches) > 4 && matches[4] != "" {
		v.Pre = matches[4]
	}

	return v, true
}

func (v Version) Compare(other Version) int {
	if v.Major != other.Major {
		return v.Major - other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor - other.Minor
	}
	if v.Patch != other.Patch {
		return v.Patch - other.Patch
	}
	return 0
}

func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	return s
}

func ParseConstraint(s string) (Constraint, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Constraint{Op: ">=", Version: Version{0, 0, 0, ""}}, true
	}

	re := regexp.MustCompile(`^(>=|<=|>|<|=|~)?\s*(.+)$`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return Constraint{}, false
	}

	op := matches[1]
	if op == "" {
		op = ">="
	}

	version, ok := ParseVersion(matches[2])
	if !ok {
		return Constraint{}, false
	}

	return Constraint{Op: op, Version: version}, true
}

func (c Constraint) Match(v Version) bool {
	cmp := v.Compare(c.Version)
	switch c.Op {
	case ">=":
		return cmp >= 0
	case ">":
		return cmp > 0
	case "<=":
		return cmp <= 0
	case "<":
		return cmp < 0
	case "=":
		return cmp == 0
	case "~":
		return v.Major == c.Version.Major && v.Minor == c.Version.Minor && cmp >= 0
	}
	return false
}

func MatchVersion(available []string, constraint string) (string, bool) {
	c, ok := ParseConstraint(constraint)
	if !ok {
		return "", false
	}

	var candidates []Version
	for _, s := range available {
		v, ok := ParseVersion(s)
		if ok && c.Match(v) {
			candidates = append(candidates, v)
		}
	}

	if len(candidates) == 0 {
		return "", false
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Compare(candidates[j]) > 0
	})

	return candidates[0].String(), true
}

func CheckCycle(path []string, current string) error {
	for _, p := range path {
		if p == current {
			return fmt.Errorf("circular dependency: %s → %s",
				strings.Join(path, " → "), current)
		}
	}
	return nil
}
