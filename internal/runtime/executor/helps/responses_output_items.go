package helps

import (
	"bytes"
	"sort"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// CollectResponsesOutputItemDone records the "item" payload of a
// "response.output_item.done" event so PatchResponsesCompletedOutput can
// reconstruct response.output on the terminal "response.completed" event when
// the upstream leaves it empty. Events without a JSON "item" are ignored. Items
// carrying an "output_index" are keyed by index; the rest are appended to the
// fallback slice in arrival order.
//
// It is shared verbatim by the Codex and xAI Responses executors.
func CollectResponsesOutputItemDone(eventData []byte, outputItemsByIndex map[int64][]byte, outputItemsFallback *[][]byte) {
	itemResult := gjson.GetBytes(eventData, "item")
	if !itemResult.Exists() || itemResult.Type != gjson.JSON {
		return
	}
	if outputIndexResult := gjson.GetBytes(eventData, "output_index"); outputIndexResult.Exists() {
		outputItemsByIndex[outputIndexResult.Int()] = []byte(itemResult.Raw)
		return
	}
	*outputItemsFallback = append(*outputItemsFallback, []byte(itemResult.Raw))
}

// PatchResponsesCompletedOutput rebuilds response.output on a
// "response.completed" event from items collected via
// CollectResponsesOutputItemDone. It only patches when response.output is
// absent or an empty array and at least one item was collected. Items are
// ordered by output_index followed by fallback arrival order, then written as a
// single raw array replacement.
//
// It is shared by the Codex and xAI Responses executors.
func PatchResponsesCompletedOutput(eventData []byte, outputItemsByIndex map[int64][]byte, outputItemsFallback [][]byte) []byte {
	outputResult := gjson.GetBytes(eventData, "response.output")
	shouldPatchOutput := (!outputResult.Exists() || !outputResult.IsArray() || len(outputResult.Array()) == 0) && (len(outputItemsByIndex) > 0 || len(outputItemsFallback) > 0)
	if !shouldPatchOutput {
		return eventData
	}

	indexes := make([]int64, 0, len(outputItemsByIndex))
	for idx := range outputItemsByIndex {
		indexes = append(indexes, idx)
	}
	sort.Slice(indexes, func(i, j int) bool {
		return indexes[i] < indexes[j]
	})

	items := make([][]byte, 0, len(outputItemsByIndex)+len(outputItemsFallback))
	for _, idx := range indexes {
		items = append(items, outputItemsByIndex[idx])
	}
	items = append(items, outputItemsFallback...)

	outputArray := []byte("[]")
	if len(items) > 0 {
		var buf bytes.Buffer
		totalLen := 2
		for _, item := range items {
			totalLen += len(item)
		}
		if len(items) > 1 {
			totalLen += len(items) - 1
		}
		buf.Grow(totalLen)
		buf.WriteByte('[')
		for i, item := range items {
			if i > 0 {
				buf.WriteByte(',')
			}
			buf.Write(item)
		}
		buf.WriteByte(']')
		outputArray = buf.Bytes()
	}

	patched, _ := sjson.SetRawBytes(eventData, "response.output", outputArray)
	return patched
}
