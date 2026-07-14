package common

import (
	"sort"

	"github.com/tidwall/sjson"
)

var (
	defaultClaudeMessageStartTemplate = []byte(`{"type":"message_start","message":{"id":"","type":"message","role":"assistant","model":"","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}`)
	defaultClaudeMessageDeltaTemplate = []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":0,"output_tokens":0}}`)
	defaultClaudeMessageStopPayload   = []byte(`{"type":"message_stop"}`)
)

// ClaudeSSEBuilderConfig controls provider-specific wire formatting.
type ClaudeSSEBuilderConfig struct {
	TrailingNewlines     int
	MessageStartTemplate []byte
	MessageDeltaTemplate []byte
	MessageStopPayload   []byte
	DefaultMessageID     string
	DefaultModel         string
}

// ClaudeMessageStartParams describes a Claude message_start payload.
type ClaudeMessageStartParams struct {
	ID    string
	Model string
	Usage ClaudeUsage
}

// ClaudeUsage contains Claude usage fields shared by target translators.
type ClaudeUsage struct {
	InputTokens       int64
	OutputTokens      int64
	CacheReadTokens   int64
	WebSearchRequests int64
}

// ClaudeMessageDeltaParams describes a Claude message_delta payload.
type ClaudeMessageDeltaParams struct {
	StopReason   string
	StopSequence string
	Usage        ClaudeUsage
}

// ClaudeSSEBuilder builds Anthropic-compatible SSE frames and tracks protocol state.
type ClaudeSSEBuilder struct {
	cfg              ClaudeSSEBuilderConfig
	nextBlockIndex   int
	openBlocks       map[int]bool
	messageStarted   bool
	messageDeltaSent bool
	messageStopSent  bool
}

// NewClaudeSSEBuilder creates a Claude SSE builder with default formatting filled in.
func NewClaudeSSEBuilder(cfg ClaudeSSEBuilderConfig) *ClaudeSSEBuilder {
	if cfg.TrailingNewlines == 0 {
		cfg.TrailingNewlines = SSEStandardTrailingNewlines
	}
	if len(cfg.MessageStartTemplate) == 0 {
		cfg.MessageStartTemplate = defaultClaudeMessageStartTemplate
	}
	if len(cfg.MessageDeltaTemplate) == 0 {
		cfg.MessageDeltaTemplate = defaultClaudeMessageDeltaTemplate
	}
	if len(cfg.MessageStopPayload) == 0 {
		cfg.MessageStopPayload = defaultClaudeMessageStopPayload
	}
	return &ClaudeSSEBuilder{
		cfg:        cfg,
		openBlocks: make(map[int]bool),
	}
}

// AppendMessageStart appends message_start once.
func (b *ClaudeSSEBuilder) AppendMessageStart(out []byte, params ClaudeMessageStartParams) []byte {
	if b.messageStarted {
		return out
	}
	id := params.ID
	if id == "" {
		id = b.cfg.DefaultMessageID
	}
	model := params.Model
	if model == "" {
		model = b.cfg.DefaultModel
	}
	payload := cloneBytes(b.cfg.MessageStartTemplate)
	payload, _ = sjson.SetBytes(payload, "message.id", id)
	payload, _ = sjson.SetBytes(payload, "message.model", model)
	payload = setClaudeUsage(payload, "message.usage", params.Usage)
	b.messageStarted = true
	return AppendSSEEventBytes(out, SSEEventMessageStart, payload, b.cfg.TrailingNewlines)
}

// AppendContentBlockStart appends content_block_start, assigning an index at open time.
func (b *ClaudeSSEBuilder) AppendContentBlockStart(out []byte, contentBlock []byte) ([]byte, int) {
	index := b.nextBlockIndex
	b.nextBlockIndex++
	return b.AppendContentBlockStartAt(out, index, contentBlock), index
}

// AppendContentBlockStartAt appends content_block_start for a provider-owned index.
func (b *ClaudeSSEBuilder) AppendContentBlockStartAt(out []byte, index int, contentBlock []byte) []byte {
	if index >= b.nextBlockIndex {
		b.nextBlockIndex = index + 1
	}
	b.openBlocks[index] = true
	payload := []byte(`{"type":"content_block_start","index":0,"content_block":{}}`)
	payload, _ = sjson.SetBytes(payload, "index", index)
	payload, _ = sjson.SetRawBytes(payload, "content_block", contentBlock)
	return AppendSSEEventBytes(out, SSEEventContentBlockStart, payload, b.cfg.TrailingNewlines)
}

// AppendTextDelta appends a text_delta for an existing content block.
func (b *ClaudeSSEBuilder) AppendTextDelta(out []byte, index int, text string) []byte {
	payload := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":""}}`)
	payload, _ = sjson.SetBytes(payload, "index", index)
	payload, _ = sjson.SetBytes(payload, "delta.text", text)
	return AppendSSEEventBytes(out, SSEEventContentBlockDelta, payload, b.cfg.TrailingNewlines)
}

// AppendThinkingDelta appends a thinking_delta for an existing content block.
func (b *ClaudeSSEBuilder) AppendThinkingDelta(out []byte, index int, thinking string) []byte {
	payload := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":""}}`)
	payload, _ = sjson.SetBytes(payload, "index", index)
	payload, _ = sjson.SetBytes(payload, "delta.thinking", thinking)
	return AppendSSEEventBytes(out, SSEEventContentBlockDelta, payload, b.cfg.TrailingNewlines)
}

// AppendSignatureDelta appends a signature_delta for an existing content block.
func (b *ClaudeSSEBuilder) AppendSignatureDelta(out []byte, index int, signature string) []byte {
	payload := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":""}}`)
	payload, _ = sjson.SetBytes(payload, "index", index)
	payload, _ = sjson.SetBytes(payload, "delta.signature", signature)
	return AppendSSEEventBytes(out, SSEEventContentBlockDelta, payload, b.cfg.TrailingNewlines)
}

// AppendInputJSONDelta appends an input_json_delta for an existing tool block.
func (b *ClaudeSSEBuilder) AppendInputJSONDelta(out []byte, index int, partialJSON string) []byte {
	payload := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":""}}`)
	payload, _ = sjson.SetBytes(payload, "index", index)
	payload, _ = sjson.SetBytes(payload, "delta.partial_json", partialJSON)
	return AppendSSEEventBytes(out, SSEEventContentBlockDelta, payload, b.cfg.TrailingNewlines)
}

// AppendRawContentBlockDeltaAt appends a provider-specific content_block_delta.
func (b *ClaudeSSEBuilder) AppendRawContentBlockDeltaAt(out []byte, index int, delta []byte) []byte {
	payload := []byte(`{"type":"content_block_delta","index":0,"delta":{}}`)
	payload, _ = sjson.SetBytes(payload, "index", index)
	payload, _ = sjson.SetRawBytes(payload, "delta", delta)
	return AppendSSEEventBytes(out, SSEEventContentBlockDelta, payload, b.cfg.TrailingNewlines)
}

// AppendContentBlockStop appends content_block_stop once for an open block.
func (b *ClaudeSSEBuilder) AppendContentBlockStop(out []byte, index int) []byte {
	if !b.openBlocks[index] {
		return out
	}
	delete(b.openBlocks, index)
	payload := []byte(`{"type":"content_block_stop","index":0}`)
	payload, _ = sjson.SetBytes(payload, "index", index)
	return AppendSSEEventBytes(out, SSEEventContentBlockStop, payload, b.cfg.TrailingNewlines)
}

// AppendTerminal closes all open blocks and appends message_delta/message_stop idempotently.
func (b *ClaudeSSEBuilder) AppendTerminal(out []byte, params ClaudeMessageDeltaParams) []byte {
	out = b.AppendOpenContentBlockStops(out)
	out = b.AppendMessageDelta(out, params)
	return b.AppendMessageStop(out)
}

// AppendOpenContentBlockStops closes all open content blocks in index order.
func (b *ClaudeSSEBuilder) AppendOpenContentBlockStops(out []byte) []byte {
	indexes := make([]int, 0, len(b.openBlocks))
	for index := range b.openBlocks {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		out = b.AppendContentBlockStop(out, index)
	}
	return out
}

// AppendMessageDelta appends message_delta once.
func (b *ClaudeSSEBuilder) AppendMessageDelta(out []byte, params ClaudeMessageDeltaParams) []byte {
	if b.messageDeltaSent {
		return out
	}
	stopReason := params.StopReason
	if stopReason == "" {
		stopReason = "end_turn"
	}
	payload := cloneBytes(b.cfg.MessageDeltaTemplate)
	payload, _ = sjson.SetBytes(payload, "delta.stop_reason", stopReason)
	if params.StopSequence != "" {
		payload, _ = sjson.SetBytes(payload, "delta.stop_sequence", params.StopSequence)
	}
	payload = setClaudeUsage(payload, "usage", params.Usage)
	b.messageDeltaSent = true
	return AppendSSEEventBytes(out, SSEEventMessageDelta, payload, b.cfg.TrailingNewlines)
}

// AppendMessageStop appends message_stop once.
func (b *ClaudeSSEBuilder) AppendMessageStop(out []byte) []byte {
	if b.messageStopSent {
		return out
	}
	b.messageStopSent = true
	return AppendSSEEventBytes(out, SSEEventMessageStop, b.cfg.MessageStopPayload, b.cfg.TrailingNewlines)
}

func setClaudeUsage(payload []byte, path string, usage ClaudeUsage) []byte {
	payload, _ = sjson.SetBytes(payload, path+".input_tokens", usage.InputTokens)
	payload, _ = sjson.SetBytes(payload, path+".output_tokens", usage.OutputTokens)
	if usage.CacheReadTokens > 0 {
		payload, _ = sjson.SetBytes(payload, path+".cache_read_input_tokens", usage.CacheReadTokens)
	}
	if usage.WebSearchRequests > 0 {
		payload, _ = sjson.SetBytes(payload, path+".server_tool_use.web_search_requests", usage.WebSearchRequests)
	}
	return payload
}

func cloneBytes(in []byte) []byte {
	out := make([]byte, len(in))
	copy(out, in)
	return out
}
