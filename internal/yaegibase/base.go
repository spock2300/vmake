package yaegibase

import (
	"fmt"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
	"github.com/traefik/yaegi/stdlib/unrestricted"
)

func New(extra interp.Exports) (*interp.Interpreter, error) {
	i := interp.New(interp.Options{})
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
