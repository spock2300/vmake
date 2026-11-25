package scriptcall

import (
	"fmt"
	"reflect"
)

// exitSignal marks a deliberate command termination. The method name is
// vmake-specific on purpose: *exec.ExitError already has ExitCode().
type exitSignal interface {
	VmakeExitCode() int
}

func Recover(err *error) {
	value := recover()
	if value == nil {
		return
	}
	if _, ok := value.(exitSignal); ok {
		panic(value)
	}
	if recovered, ok := value.(error); ok && !isNilError(recovered) {
		*err = recovered
	} else {
		*err = fmt.Errorf("script panic: %v", value)
	}
}

func isNilError(err error) bool {
	value := reflect.ValueOf(err)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	}
	return false
}

func Run(fn func()) (err error) {
	defer Recover(&err)
	fn()
	return nil
}
