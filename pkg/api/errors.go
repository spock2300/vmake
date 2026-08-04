package api

import "fmt"

type BuildScriptError struct {
	Package string
	Op      string
	Err     error
}

func (e *BuildScriptError) Error() string {
	if e.Package == "" {
		if e.Op == "" {
			return e.Err.Error()
		}
		return fmt.Sprintf("%s: %v", e.Op, e.Err)
	}
	if e.Op == "" {
		return fmt.Sprintf("package %s: %v", e.Package, e.Err)
	}
	return fmt.Sprintf("package %s: %s: %v", e.Package, e.Op, e.Err)
}

func (e *BuildScriptError) Unwrap() error { return e.Err }

func fatalScript(pkgName, op string, format string, args ...any) {
	panic(&BuildScriptError{
		Package: pkgName,
		Op:      op,
		Err:     fmt.Errorf(format, args...),
	})
}
