package issue

import "errors"

var ErrCircularDependency = errors.New("circular dependency")
