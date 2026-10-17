package tui

import (
	"fmt"
	"strconv"
)

func formatValue(v any) string {
	switch n := v.(type) {
	case float32:
		return strconv.FormatFloat(float64(n), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	return fmt.Sprintf("%v", v)
}
