package yaegisym

import "reflect"

//go:generate yaegi extract github.com/spf13/cobra github.com/spf13/pflag

var Symbols = map[string]map[string]reflect.Value{}
