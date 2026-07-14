// Package clone provides generic deep cloning utility functions.
package clone

// AnyMap performs a recursive deep clone of map[string]any.
// Supports map[string]any, []any, []string, and basic types (returned directly).
func AnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = AnyValue(value)
	}
	return dst
}

// AnyValue performs a recursive deep clone of any type.
// Supports map[string]any, []any, []string, and basic types (returned directly).
func AnyValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return AnyMap(v)
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = AnyValue(v[i])
		}
		return out
	case []string:
		return append([]string(nil), v...)
	default:
		return v
	}
}
