package pipeline

import (
	"fmt"
	"maps"
	"slices"

	"github.com/spock2300/vmake/pkg/api"
)

// OptionProfile selects one of the documented option projections.
type OptionProfile int

const (
	// OptionProfileInspect projects inspect's packages[].options: options the
	// package declares that carry a value, globals it declares included, with
	// project globals neither merged nor defaulted.
	OptionProfileInspect OptionProfile = iota
	// OptionProfileQuery projects the query tree line: globals skipped, project
	// globals merged for lookup, defaults filled.
	OptionProfileQuery
)

// OptionValue is one projected option of one package.
type OptionValue struct {
	Name     string
	Resolved bool
	Value    any
}

// OptionProjection is the input of an option-value projection.
type OptionProjection struct {
	Values       map[string]any
	Defs         map[string]*api.Option
	GlobalDefs   map[string]*api.Option
	GlobalValues map[string]any
}

// Project returns the package's options under the requested profile.
func (p OptionProjection) Project(profile OptionProfile) []OptionValue {
	switch profile {
	case OptionProfileInspect, OptionProfileQuery:
	default:
		panic(fmt.Sprintf("pipeline: unknown option profile %d", profile))
	}
	if len(p.Defs) == 0 {
		return nil
	}
	query := profile == OptionProfileQuery
	defs, values := p.Defs, p.Values
	if query {
		defs = maps.Clone(defs)
		values = maps.Clone(values)
	}
	acc := api.NewConfigAccessor(values, defs)
	if query {
		acc.MergeGlobals(p.GlobalDefs, p.GlobalValues)
	}
	names := make([]string, 0, len(p.Defs))
	for name := range p.Defs {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]OptionValue, 0, len(names))
	for _, name := range names {
		def := p.Defs[name]
		if def == nil {
			continue
		}
		if query && def.IsGlobal() {
			continue
		}
		value := OptionValue{Name: name}
		if !query {
			raw, exists := p.Values[name]
			if !exists || raw == nil {
				out = append(out, value)
				continue
			}
		}
		value.Resolved = true
		value.Value = typedOptionValue(&acc, def.Type(), name)
		out = append(out, value)
	}
	return out
}

func typedOptionValue(acc *api.ConfigAccessor, typ api.OptionType, name string) any {
	switch typ {
	case api.OptionBool:
		return acc.Bool(name)
	case api.OptionInt:
		return acc.Int(name)
	default:
		return acc.String(name)
	}
}
