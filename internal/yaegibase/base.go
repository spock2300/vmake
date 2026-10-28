package yaegibase

import (
	"fmt"
	"io"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
	"github.com/traefik/yaegi/stdlib/unrestricted"
)

func New(extra interp.Exports) (*interp.Interpreter, error) {
	return NewWithOptions(interp.Options{Stderr: io.Discard}, extra)
}

// NewWithOptions is New with caller-provided interpreter options. Build
// scripts use it to expose local Go packages through a virtual GOPATH while
// keeping yaegi's default stderr sink.
func NewWithOptions(opts interp.Options, extra interp.Exports) (*interp.Interpreter, error) {
	i := interp.New(opts)
	if err := i.Use(stdlib.Symbols); err != nil {
		return nil, fmt.Errorf("yaegi use stdlib: %w", err)
	}
	if err := i.Use(unrestricted.Symbols); err != nil {
		return nil, fmt.Errorf("yaegi use unrestricted: %w", err)
	}
	if err := i.Use(extra); err != nil {
		return nil, fmt.Errorf("yaegi use extra symbols: %w", err)
	}
	return i, nil
}
