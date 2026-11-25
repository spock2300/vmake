// Package issue holds error sentinels that producers and consumers in different
// layers share without exposing them to interpreted scripts.
package issue

import "errors"

// ErrCircularDependency marks a dependency cycle.
var ErrCircularDependency = errors.New("circular dependency")
