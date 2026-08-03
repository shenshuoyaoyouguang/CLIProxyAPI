package thinking

import (
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ObjectSubtreeRaw returns the raw JSON of the object under path, or an empty
// object when the subtree is absent, null, or a scalar (sjson promotes those
// to an object when a child key is set). writable is false only for
// array-valued subtrees, which sjson rejects; the caller must then leave the
// subtree untouched.
//
// The three provider appliers that edit a nested thinking object (Claude and
// Kimi at "thinking", Gemini at "generationConfig.thinkingConfig") share this
// helper so the array-rejection / scalar-promotion semantics are defined once.
func ObjectSubtreeRaw(body []byte, path string) (raw []byte, writable bool) {
	sub := gjson.GetBytes(body, path)
	switch {
	case sub.IsObject():
		return []byte(sub.Raw), true
	case sub.IsArray():
		return nil, false
	default:
		return []byte(`{}`), true
	}
}

// SetObjectSubtreeRaw replaces (or creates) the object under path in a single
// full-body pass.
func SetObjectSubtreeRaw(body []byte, path string, raw []byte) []byte {
	out, _ := sjson.SetRawBytes(body, path, raw)
	return out
}
