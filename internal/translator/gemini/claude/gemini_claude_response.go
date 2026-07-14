// Package claude provides response translation functionality for Claude API.
// This package handles the conversion of backend client responses into Claude-compatible
// Server-Sent Events (SSE) format, implementing a sophisticated state machine that manages
// different response types including text content, thinking processes, and function calls.
// The translation ensures proper sequencing of SSE events and maintains state across
// multiple response chunks to provide a seamless streaming experience.
package claude

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	translatorcommon "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/common"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Params holds parameters for response conversion.
type Params struct {
	IsGlAPIKey       bool
	HasFirstResponse bool
	ResponseType     int
	ResponseIndex    int
	HasContent       bool // Tracks whether any content (text, thinking, or tool use) has been output
	ToolNameMap      map[string]string
	SanitizedNameMap map[string]string
	SawToolCall      bool
	HasFinalEvents   bool
	Builder          *translatorcommon.ClaudeSSEBuilder
}

// toolUseIDCounter provides a process-wide unique counter for tool use identifiers.
var toolUseIDCounter uint64

func newGeminiClaudeSSEBuilder() *translatorcommon.ClaudeSSEBuilder {
	return translatorcommon.NewClaudeSSEBuilder(translatorcommon.ClaudeSSEBuilderConfig{
		MessageStartTemplate: []byte(`{"type":"message_start","message":{"id":"msg_1nZdL29xx5MUA1yADyHTEsnR8uuvGzszyY","type":"message","role":"assistant","content":[],"model":"claude-3-5-sonnet-20241022","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}`),
	})
}

func ensureGeminiClaudeBuilder(params *Params) *translatorcommon.ClaudeSSEBuilder {
	if params.Builder == nil {
		params.Builder = newGeminiClaudeSSEBuilder()
	}
	return params.Builder
}

// ConvertGeminiResponseToClaude performs sophisticated streaming response format conversion.
// This function implements a complex state machine that translates backend client responses
// into Claude-compatible Server-Sent Events (SSE) format. It manages different response types
// and handles state transitions between content blocks, thinking processes, and function calls.
//
// Response type states: 0=none, 1=content, 2=thinking, 3=function
// The function maintains state across multiple calls to ensure proper SSE event sequencing.
//
// Parameters:
//   - ctx: The context for the request.
//   - modelName: The name of the model.
//   - rawJSON: The raw JSON response from the Gemini API.
//   - param: A pointer to a parameter object for the conversion.
//
// Returns:
//   - [][]byte: A slice of bytes, each containing a Claude-compatible SSE payload.
func ConvertGeminiResponseToClaude(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	if *param == nil {
		*param = &Params{
			IsGlAPIKey:       false,
			HasFirstResponse: false,
			ResponseType:     0,
			ResponseIndex:    0,
			ToolNameMap:      util.ToolNameMapFromClaudeRequest(originalRequestRawJSON),
			SanitizedNameMap: util.SanitizedToolNameMap(originalRequestRawJSON),
			SawToolCall:      false,
			Builder:          newGeminiClaudeSSEBuilder(),
		}
	}
	params := (*param).(*Params)
	ensureGeminiClaudeBuilder(params)

	if bytes.Equal(rawJSON, []byte("[DONE]")) {
		// Only send message_stop if we have actually output content
		if params.HasContent {
			return [][]byte{params.Builder.AppendMessageStop(nil)}
		}
		return [][]byte{}
	}

	output := make([]byte, 0, 1024)
	appendSignatureDelta := func(signature string) {
		if signature == "" || params.ResponseType != 2 {
			return
		}
		output = params.Builder.AppendSignatureDelta(output, params.ResponseIndex, signature)
		params.HasContent = true
	}

	// Initialize the streaming session with a message_start event
	// This is only sent for the very first response chunk
	if !params.HasFirstResponse {
		// Create the initial message structure with default values
		// This follows the Claude API specification for streaming message initialization
		messageID := "msg_1nZdL29xx5MUA1yADyHTEsnR8uuvGzszyY"
		model := "claude-3-5-sonnet-20241022"

		// Override default values with actual response metadata if available
		if modelVersionResult := gjson.GetBytes(rawJSON, "modelVersion"); modelVersionResult.Exists() {
			model = modelVersionResult.String()
		}
		if responseIDResult := gjson.GetBytes(rawJSON, "responseId"); responseIDResult.Exists() {
			messageID = responseIDResult.String()
		}
		output = params.Builder.AppendMessageStart(output, translatorcommon.ClaudeMessageStartParams{ID: messageID, Model: model})

		params.HasFirstResponse = true
	}

	// Process the response parts array from the backend client
	// Each part can contain text content, thinking content, or function calls
	partsResult := gjson.GetBytes(rawJSON, "candidates.0.content.parts")
	if partsResult.IsArray() {
		partResults := partsResult.Array()
		for i := 0; i < len(partResults); i++ {
			partResult := partResults[i]

			// Extract the different types of content from each part
			partTextResult := partResult.Get("text")
			functionCallResult := partResult.Get("functionCall")
			thoughtSignatureResult := partResult.Get("thoughtSignature")
			if !thoughtSignatureResult.Exists() {
				thoughtSignatureResult = partResult.Get("thought_signature")
			}
			hasThoughtSignature := thoughtSignatureResult.Exists() && thoughtSignatureResult.String() != ""

			if hasThoughtSignature && !partTextResult.Exists() && !functionCallResult.Exists() {
				appendSignatureDelta(thoughtSignatureResult.String())
				continue
			}

			// Handle text content (both regular content and thinking)
			if partTextResult.Exists() {
				// Process thinking content (internal reasoning)
				if partResult.Get("thought").Bool() || hasThoughtSignature {
					if hasThoughtSignature && partTextResult.String() == "" {
						appendSignatureDelta(thoughtSignatureResult.String())
						continue
					}
					// Continue existing thinking block
					if params.ResponseType == 2 {
						output = params.Builder.AppendThinkingDelta(output, params.ResponseIndex, partTextResult.String())
						params.HasContent = true
					} else {
						// Transition from another state to thinking
						// First, close any existing content block
						if params.ResponseType != 0 {
							if params.ResponseType == 2 {
								// output = output + "event: content_block_delta\n"
								// output = output + fmt.Sprintf(`data: {"type":"content_block_delta","index":%d,"delta":{"type":"signature_delta","signature":null}}`, (*param).(*Params).ResponseIndex)
								// output = output + "\n\n\n"
							}
							output = params.Builder.AppendContentBlockStop(output, params.ResponseIndex)
							params.ResponseIndex++
						}

						// Start a new thinking content block
						output = params.Builder.AppendContentBlockStartAt(output, params.ResponseIndex, []byte(`{"type":"thinking","thinking":""}`))
						output = params.Builder.AppendThinkingDelta(output, params.ResponseIndex, partTextResult.String())
						params.ResponseType = 2 // Set state to thinking
						params.HasContent = true
					}
					appendSignatureDelta(thoughtSignatureResult.String())
				} else {
					// Process regular text content (user-visible output)
					// Continue existing text block
					if params.ResponseType == 1 {
						output = params.Builder.AppendTextDelta(output, params.ResponseIndex, partTextResult.String())
						params.HasContent = true
					} else {
						// Transition from another state to text content
						// First, close any existing content block
						if params.ResponseType != 0 {
							if params.ResponseType == 2 {
								// output = output + "event: content_block_delta\n"
								// output = output + fmt.Sprintf(`data: {"type":"content_block_delta","index":%d,"delta":{"type":"signature_delta","signature":null}}`, (*param).(*Params).ResponseIndex)
								// output = output + "\n\n\n"
							}
							output = params.Builder.AppendContentBlockStop(output, params.ResponseIndex)
							params.ResponseIndex++
						}

						// Start a new text content block
						output = params.Builder.AppendContentBlockStartAt(output, params.ResponseIndex, []byte(`{"type":"text","text":""}`))
						output = params.Builder.AppendTextDelta(output, params.ResponseIndex, partTextResult.String())
						params.ResponseType = 1 // Set state to content
						params.HasContent = true
					}
				}
			} else if functionCallResult.Exists() {
				// Handle function/tool calls from the AI model
				// This processes tool usage requests and formats them for Claude API compatibility
				params.SawToolCall = true
				upstreamToolName := functionCallResult.Get("name").String()
				upstreamToolName = util.RestoreSanitizedToolName(params.SanitizedNameMap, upstreamToolName)
				clientToolName := util.MapToolName(params.ToolNameMap, upstreamToolName)

				// FIX: Handle streaming split/delta where name might be empty in subsequent chunks.
				// If we are already in tool use mode and name is empty, treat as continuation (delta).
				if params.ResponseType == 3 && upstreamToolName == "" {
					if fcArgsResult := functionCallResult.Get("args"); fcArgsResult.Exists() {
						output = params.Builder.AppendInputJSONDelta(output, params.ResponseIndex, fcArgsResult.Raw)
					}
					// Continue to next part without closing/opening logic
					continue
				}

				// Handle state transitions when switching to function calls
				// Close any existing function call block first
				if params.ResponseType == 3 {
					output = params.Builder.AppendContentBlockStop(output, params.ResponseIndex)
					params.ResponseIndex++
					params.ResponseType = 0
				}

				// Special handling for thinking state transition
				if params.ResponseType == 2 {
					// output = output + "event: content_block_delta\n"
					// output = output + fmt.Sprintf(`data: {"type":"content_block_delta","index":%d,"delta":{"type":"signature_delta","signature":null}}`, (*param).(*Params).ResponseIndex)
					// output = output + "\n\n\n"
				}

				// Close any other existing content block
				if params.ResponseType != 0 {
					output = params.Builder.AppendContentBlockStop(output, params.ResponseIndex)
					params.ResponseIndex++
				}

				// Start a new tool use content block
				// This creates the structure for a function call in Claude format
				// Create the tool use block with unique ID and function details
				contentBlock := []byte(`{"type":"tool_use","id":"","name":"","input":{}}`)
				contentBlock, _ = sjson.SetBytes(contentBlock, "id", util.SanitizeClaudeToolID(fmt.Sprintf("%s-%d", upstreamToolName, atomic.AddUint64(&toolUseIDCounter, 1))))
				contentBlock, _ = sjson.SetBytes(contentBlock, "name", clientToolName)
				output = params.Builder.AppendContentBlockStartAt(output, params.ResponseIndex, contentBlock)

				if fcArgsResult := functionCallResult.Get("args"); fcArgsResult.Exists() {
					output = params.Builder.AppendInputJSONDelta(output, params.ResponseIndex, fcArgsResult.Raw)
				}
				params.ResponseType = 3
				params.HasContent = true
			}
		}
	}

	usageResult := gjson.GetBytes(rawJSON, "usageMetadata")
	if usageResult.Exists() && bytes.Contains(rawJSON, []byte(`"finishReason"`)) && !params.HasFinalEvents {
		// Only send final events if we have actually output content
		if params.HasContent {
			if params.ResponseType != 0 {
				output = params.Builder.AppendContentBlockStop(output, params.ResponseIndex)
				params.ResponseType = 0
			}

			thoughtsTokenCount := usageResult.Get("thoughtsTokenCount").Int()
			candidatesTokenCount := usageResult.Get("candidatesTokenCount").Int()
			output = params.Builder.AppendMessageDelta(output, translatorcommon.ClaudeMessageDeltaParams{
				StopReason: translatorcommon.MapGeminiFinishReasonToClaude(gjson.GetBytes(rawJSON, "candidates.0.finishReason").String(), params.SawToolCall),
				Usage: translatorcommon.ClaudeUsage{
					InputTokens:  usageResult.Get("promptTokenCount").Int(),
					OutputTokens: candidatesTokenCount + thoughtsTokenCount,
				},
			})
			params.HasFinalEvents = true
		}
	}

	return [][]byte{output}
}

// ConvertGeminiResponseToClaudeNonStream converts a non-streaming Gemini response to a non-streaming Claude response.
//
// Parameters:
//   - ctx: The context for the request.
//   - modelName: The name of the model.
//   - rawJSON: The raw JSON response from the Gemini API.
//   - param: A pointer to a parameter object for the conversion.
//
// Returns:
//   - []byte: A Claude-compatible JSON response.
func ConvertGeminiResponseToClaudeNonStream(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) []byte {
	_ = requestRawJSON

	root := gjson.ParseBytes(rawJSON)
	toolNameMap := util.ToolNameMapFromClaudeRequest(originalRequestRawJSON)
	sanitizedNameMap := util.SanitizedToolNameMap(originalRequestRawJSON)

	out := []byte(`{"id":"","type":"message","role":"assistant","model":"","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}`)
	out, _ = sjson.SetBytes(out, "id", root.Get("responseId").String())
	out, _ = sjson.SetBytes(out, "model", root.Get("modelVersion").String())

	inputTokens := root.Get("usageMetadata.promptTokenCount").Int()
	outputTokens := root.Get("usageMetadata.candidatesTokenCount").Int() + root.Get("usageMetadata.thoughtsTokenCount").Int()
	out, _ = sjson.SetBytes(out, "usage.input_tokens", inputTokens)
	out, _ = sjson.SetBytes(out, "usage.output_tokens", outputTokens)

	parts := root.Get("candidates.0.content.parts")
	textBuilder := strings.Builder{}
	thinkingBuilder := strings.Builder{}
	toolIDCounter := 0
	hasToolCall := false

	flushText := func() {
		if textBuilder.Len() == 0 {
			return
		}
		block := []byte(`{"type":"text","text":""}`)
		block, _ = sjson.SetBytes(block, "text", textBuilder.String())
		out, _ = sjson.SetRawBytes(out, "content.-1", block)
		textBuilder.Reset()
	}

	flushThinking := func() {
		if thinkingBuilder.Len() == 0 {
			return
		}
		block := []byte(`{"type":"thinking","thinking":""}`)
		block, _ = sjson.SetBytes(block, "thinking", thinkingBuilder.String())
		out, _ = sjson.SetRawBytes(out, "content.-1", block)
		thinkingBuilder.Reset()
	}

	if parts.IsArray() {
		for _, part := range parts.Array() {
			if text := part.Get("text"); text.Exists() && text.String() != "" {
				if part.Get("thought").Bool() {
					flushText()
					thinkingBuilder.WriteString(text.String())
					continue
				}
				flushThinking()
				textBuilder.WriteString(text.String())
				continue
			}

			if functionCall := part.Get("functionCall"); functionCall.Exists() {
				flushThinking()
				flushText()
				hasToolCall = true

				upstreamToolName := functionCall.Get("name").String()
				upstreamToolName = util.RestoreSanitizedToolName(sanitizedNameMap, upstreamToolName)
				clientToolName := util.MapToolName(toolNameMap, upstreamToolName)
				toolIDCounter++
				toolBlock := []byte(`{"type":"tool_use","id":"","name":"","input":{}}`)
				toolBlock, _ = sjson.SetBytes(toolBlock, "id", util.SanitizeClaudeToolID(fmt.Sprintf("%s-%d", upstreamToolName, toolIDCounter)))
				toolBlock, _ = sjson.SetBytes(toolBlock, "name", clientToolName)
				inputRaw := "{}"
				if args := functionCall.Get("args"); args.Exists() && gjson.Valid(args.Raw) && args.IsObject() {
					inputRaw = args.Raw
				}
				toolBlock, _ = sjson.SetRawBytes(toolBlock, "input", []byte(inputRaw))
				out, _ = sjson.SetRawBytes(out, "content.-1", toolBlock)
				continue
			}
		}
	}

	flushThinking()
	flushText()

	stopReason := translatorcommon.MapGeminiFinishReasonToClaude(root.Get("candidates.0.finishReason").String(), hasToolCall)
	out, _ = sjson.SetBytes(out, "stop_reason", stopReason)

	if inputTokens == int64(0) && outputTokens == int64(0) && !root.Get("usageMetadata").Exists() {
		out, _ = sjson.DeleteBytes(out, "usage")
	}

	return out
}

func ClaudeTokenCount(ctx context.Context, count int64) []byte {
	return translatorcommon.ClaudeInputTokensJSON(count)
}
