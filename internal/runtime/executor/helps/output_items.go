package helps

import (
	"bytes"
	"maps"
	"slices"
)

// OrderedOutputItems returns the items indexed by output_index in ascending
// index order followed by the fallback items, mirroring the ordering the
// response.output_item.done collectors rely on when rebuilding a response.
func OrderedOutputItems(byIndex map[int64][]byte, fallback [][]byte) [][]byte {
	items := make([][]byte, 0, len(byIndex)+len(fallback))
	if len(byIndex) > 0 {
		for _, idx := range slices.Sorted(maps.Keys(byIndex)) {
			items = append(items, byIndex[idx])
		}
	}
	return append(items, fallback...)
}

// BuildOutputArray renders items as a JSON array of raw byte payloads,
// returning "[]" when no items are present.
func BuildOutputArray(items [][]byte) []byte {
	if len(items) == 0 {
		return []byte("[]")
	}
	totalLen := 2
	for _, item := range items {
		totalLen += len(item)
	}
	totalLen += len(items) - 1 // separators
	var buf bytes.Buffer
	buf.Grow(totalLen)
	buf.WriteByte('[')
	for i, item := range items {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(item)
	}
	buf.WriteByte(']')
	return buf.Bytes()
}
