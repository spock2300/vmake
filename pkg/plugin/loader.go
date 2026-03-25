package plugin

import (
	"fmt"
	"plugin"
	"reflect"
)

type LoadedPlugin struct {
	Info   *Info
	Entry  MainFunc
	SoPath string
}

func Load(soPath string) (*LoadedPlugin, error) {
	p, err := plugin.Open(soPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open plugin: %w", err)
	}

	sym, err := p.Lookup("Main")
	if err != nil {
		return nil, fmt.Errorf("failed to lookup Main function: %w", err)
	}

	fn := reflect.ValueOf(sym)
	if fn.Kind() != reflect.Func {
		return nil, fmt.Errorf("Main is not a function")
	}

	ft := fn.Type()
	if ft.NumIn() != 1 {
		return nil, fmt.Errorf("Main should take exactly one argument")
	}

	ctxType := ft.In(0)
	if ctxType.Kind() != reflect.Ptr {
		return nil, fmt.Errorf("Main argument should be a pointer")
	}

	mainFunc := func(ctx *Context) {
		ctxVal := reflect.ValueOf(ctx)
		fn.Call([]reflect.Value{ctxVal})
	}

	return &LoadedPlugin{
		Entry:  mainFunc,
		SoPath: soPath,
	}, nil
}
