package glob

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
)

func Match(pattern, dir string) ([]string, error) {
	pattern = path.Clean(filepath.ToSlash(pattern))
	if err := validatePattern(pattern); err != nil {
		return nil, err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	var matches []string
	if strings.Contains(pattern, "**") {
		matches, err = matchDoubleStar(pattern, absDir)
	} else {
		matches, err = matchSingleStar(pattern, absDir)
	}
	if err != nil {
		return nil, err
	}

	var result []string
	for _, m := range matches {
		rel, err := filepath.Rel(absDir, m)
		if err != nil {
			result = append(result, m)
		} else {
			result = append(result, rel)
		}
	}

	return result, nil
}

func matchSingleStar(pattern, dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return nil, err
	}

	var result []string
	for _, match := range matches {
		abs, err := filepath.Abs(match)
		if err != nil {
			continue
		}
		result = append(result, abs)
	}

	return result, nil
}

func matchDoubleStar(pattern, dir string) ([]string, error) {
	var result []string
	parts := strings.Split(pattern, "/")
	prefixLen := 0
	for prefixLen < len(parts) && !strings.ContainsAny(parts[prefixLen], "*?[\\") {
		prefixLen++
	}
	baseDir := filepath.Join(dir, filepath.FromSlash(strings.Join(parts[:prefixLen], "/")))

	err := fs.Walk(baseDir, func(file string, info os.FileInfo, err error) error {
		if err != nil {
			if file == baseDir && os.IsNotExist(err) {
				return nil
			}
			if info != nil && info.Mode()&os.ModeSymlink != 0 && os.IsNotExist(err) {
				rel, relErr := filepath.Rel(dir, file)
				if relErr != nil {
					return relErr
				}
				if !MatchPath(pattern, rel) {
					return nil
				}
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, file)
		if err != nil {
			return err
		}
		if MatchPath(pattern, rel) {
			result = append(result, file)
		}
		return nil
	})
	sort.Strings(result)
	return result, err
}

func MatchPath(pattern, name string) bool {
	pattern = path.Clean(filepath.ToSlash(pattern))
	name = path.Clean(filepath.ToSlash(name))
	if err := validatePattern(pattern); err != nil {
		return false
	}
	return matchParts(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func validatePattern(pattern string) error {
	for _, part := range strings.Split(pattern, "/") {
		if _, err := path.Match(part, ""); err != nil {
			return err
		}
	}
	return nil
}

func matchParts(pattern, name []string) bool {
	if len(pattern) == 0 {
		return len(name) == 0
	}
	if pattern[0] == "**" {
		for i := 0; i <= len(name); i++ {
			if matchParts(pattern[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	matched, err := path.Match(pattern[0], name[0])
	return err == nil && matched && matchParts(pattern[1:], name[1:])
}

func IsCppFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".cpp" || ext == ".cc" || ext == ".cxx" || ext == ".C"
}
