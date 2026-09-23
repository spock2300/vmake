package scriptcall

import (
	"fmt"
	"reflect"
)

func Recover(err *error) {
	if value := recover(); value != nil {
		if recovered, ok := value.(error); ok && !isNilError(recovered) {
			*err = recovered
		} else {
			*err = fmt.Errorf("script panic: %v", value)
		}
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
