package glob

import (
	"os"
	"path/filepath"
	"strings"
)

func Match(pattern, dir string) ([]string, error) {
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

	parts := strings.Split(pattern, "**")
	if len(parts) != 2 {
		return nil, nil
	}

	prefix := strings.TrimSuffix(parts[0], "/")
	suffix := strings.TrimPrefix(parts[1], "/")

	baseDir := dir
	if prefix != "" && prefix != "." {
		baseDir = filepath.Join(dir, prefix)
	}

	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}

		if suffix == "" {
			result = append(result, path)
			return nil
		}

		rel, err := filepath.Rel(baseDir, path)
		if err != nil {
			return nil
		}

		matched, err := filepath.Match(suffix, rel)
		if err != nil {
			return nil
		}
		if matched {
			result = append(result, path)
		}

		return nil
	})

	return result, err
}

func MatchPath(pattern, path string) bool {
	if !strings.Contains(pattern, "**") {
		matched, err := filepath.Match(pattern, path)
		return err == nil && matched
	}

	parts := strings.SplitN(pattern, "**", 2)
	if len(parts) != 2 {
		return false
	}

	prefix := strings.TrimSuffix(parts[0], "/")
	suffix := strings.TrimPrefix(parts[1], "/")

	if prefix != "" && prefix != "." {
		if !strings.HasPrefix(path, prefix+"/") && path != prefix {
			return false
		}
		path = strings.TrimPrefix(path, prefix+"/")
	}

	if suffix == "" {
		return true
	}

	for {
		if matched, _ := filepath.Match(suffix, path); matched {
			return true
		}
		idx := strings.Index(path, "/")
		if idx < 0 {
			break
		}
		path = path[idx+1:]
	}

	return false
}

func IsCppFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".cpp" || ext == ".cc" || ext == ".cxx" || ext == ".C"
}
