# Fix: `internal/translator` third-batch audit remediation (P2 #18–21, #26 + P3 ×6)

> Goal: remediate the boundary/cleanup findings from the code review report that
> live inside `internal/translator/`. These were part of the third audit batch.
> They could not be applied directly in-session because the working account has
> **READ** (not WRITE/MAINTAIN/ADMIN) permission on `router-for-me/CLIProxyAPI`
> (per `AGENTS.md`, translator changes require WRITE+ on the repo). The intended
> implementation for each item is included below so a maintainer can land it.

## P2 — functional correctness

### #18 — `custom_tool_call` input must be encoded as valid JSON (`SetRawBytes`, not `SetBytes`)
- File: `internal/translator/openai/openai/responses/openai_openai-responses_response.go`
  (custom_tool_call item assembly at lines ~176, ~326, ~654–661) and the tool
  definition helper `convertResponsesCustomToolToOpenAIChat`
  (`openai/openai/responses/openai_openai-responses_tools.go:32`).
- Bug: when a Responses `custom_tool_call` carries a structured `input`, the value
  is written with `sjson.SetBytes(...)` (which JSON-escapes it into a string) or
  assembled as a literal `""`. The downstream Chat-Completions consumer expects a
  JSON object/string embedded as raw JSON.
- Intended fix: build the `input` value as valid JSON bytes and embed with
  `sjson.SetRawBytes(...)` (mirroring how `convertResponsesFunctionToolToOpenAIChat`
  already embeds `parameters` via `SetRawBytes` at `openai_openai-responses_tools.go:89`).
  ```go
  // desired: keep input as raw JSON, never re-escape
  if inputRaw := gjson.GetBytes(item.Get("input").Raw, "@this"); inputRaw.Exists() {
      item, _ = sjson.SetRawBytes(item, "input", []byte(inputRaw.Raw))
  }
  ```
- Impact: correct `custom_tool_call` arguments for Codex/OpenAI Chat consumers.

### #19 — image `tool_result` with array `content` degrades to text instead of dropping
- File: `internal/translator/interactions/claude/interactions_claude_request.go`
  `claudeToolResultToInteractions` (lines 216–243).
- Bug: when `part.content` is an array, only `type=="text"` parts are kept; image /
  document parts are silently discarded, losing tool-result data.
- Intended fix: for non-text parts (image/document/file), emit a text fallback
  (e.g. a descriptive placeholder or the part's caption) so the result is never
  empty:
  ```go
  result.ForEach(func(_, item gjson.Result) bool {
      switch item.Get("type").String() {
      case "text":
          // existing handling
      case "image", "document", "file":
          // degrade to text so the tool result is preserved
          text := item.Get("text").String()
          if text == "" { text = fmt.Sprintf("[%s payload omitted]", item.Get("type").String()) }
          contentPart, _ := sjson.SetBytes([]byte(`{"type":"text","text":""}`), "text", text)
          contentItems = append(contentItems, contentPart)
      }
      return true
  })
  ```
- Impact: interactions tool results keep image/document content as text.

### #20 — non-streaming response must concatenate all content parts, not only the first
- File: response translators that, for non-streaming payloads, take
  `content.0` only (e.g. `claude/openai/responses/claude_openai-responses_response.go`
  `SetBytes(final, "item.content.0.text", fullText)` at ~195, and the gemini
  equivalent `gemini/openai/responses/gemini_openai-responses_response.go:~296`).
- Bug: when the upstream non-streaming response has multiple text parts, only the
  first is surfaced; the rest are dropped.
- Intended fix: join every text part into `fullText` before writing, e.g.
  ```go
  var buf strings.Builder
  contentParts.ForEach(func(_, p gjson.Result) bool {
      if t := p.Get("text").String(); t != "" {
          if buf.Len() > 0 { buf.WriteByte('\n') }
          buf.WriteString(t)
      }
      return true
  })
  final, _ = sjson.SetBytes(final, "item.content.0.text", buf.String())
  ```
- Impact: multi-part non-streaming answers render fully.

### #21 — streaming tool-call block index conflict: close an unfinished function-call block first
- File: `internal/translator/codex/claude/codex_claude_response.go`
  (`appendCodexFunctionCallStart`/`Stop` at ~630–645) and
  `internal/translator/openai/claude/openai_claude_response.go`
  (`toolContentBlockIndex` at ~512, `ToolCallBlockIndexes`).
- Bug: if a new function-call block starts while a previous one is still open
  (its `content_block_stop` not yet emitted), indices collide and the wire stream
  becomes malformed. `TestConvertCodexResponseToClaude_StreamDeferredUnnamedFunctionCallDoesNotReserveBlockIndex`
  already guards the reservation side; the emit side needs the matching guard.
- Intended fix: before emitting `content_block_start` for a new tool call, emit
  `content_block_stop` for any tool block still marked open in the accumulator /
  `ToolCallBlockIndexes`.
  ```go
  // before opening a new tool block:
  for idx, open := range st.openToolBlockIndexes {
      if open {
          out = append(out, appendCodexFunctionCallStop(out, idx)...)
          st.openToolBlockIndexes[idx] = false
      }
  }
  ```
- Impact: well-formed streaming tool-call sequences under interleaved calls.

### #26 — `fixCLIToolResponse` drops responses and mis-associates via FIFO-by-count
- File: `internal/translator/antigravity/gemini/antigravity_gemini_request.go`
  `fixCLIToolResponse` (lines 542–696).
- Bug: responses are matched to function-call groups by **count** (FIFO:
  `groupResponses := collectedResponses[:group.ResponsesNeeded]`), not by the
  call **name/id**. When call/response counts or order differ, responses attach to
  the wrong calls, and any surplus response is silently dropped.
- Intended fix: match each `functionResponse` to the pending group whose
  `CallNames` contains the response's `functionResponse.name`; only close a group
  once all its named calls are satisfied; never discard a response that has a
  matching pending call.
  ```go
  // match by name, not by FIFO slice
  for len(pendingGroups) > 0 {
      group := pendingGroups[0]
      needed := group.ResponsesNeeded
      matched := 0
      for i := 0; i < len(collectedResponses); {
          name := collectedResponses[i].Get("functionResponse.name").String()
          if contains(group.CallNames, name) {
              groupResponses = append(groupResponses, collectedResponses[i])
              collectedResponses = append(collectedResponses[:i], collectedResponses[i+1:]...)
              matched++
              if matched == needed { break }
          } else {
              i++
          }
      }
      if matched == needed {
          appendFunctionResponses(groupResponses, group.CallNames)
          pendingGroups = pendingGroups[1:]
      } else {
          break // wait for more responses
      }
  }
  ```
- Impact: correct CLI tool-call/response grouping for Antigravity→Gemini.

## P3 — robustness / cleanup

### #27 — `reasoning_summary_text.done` should not hardcode a trailing newline
- Files: `claude/openai/responses/claude_openai-responses_response.go` (~398–403,
  ~448) and `gemini/openai/responses/gemini_openai-responses_response.go`
  (~211–216, ~230).
- Bug: the `response.reasoning_summary_text.delta` events and the `.done` event
  hardcode `delta:""` / `text:""` and rely on buffer concatenation that can append
  an extra `\n`; the `.done` `text` must equal exactly the concatenated deltas with
  no extra hardcoded newline.
- Intended fix: stream the real delta text (not `""`) and set `.done` `text` to the
  exact buffer without appending a trailing newline.

### #28 — strict `data:` SSE prefix matching (centralized)
- Files: `interactions/claude/interactions_claude_response.go:313,319`,
  `antigravity/interactions/interactions_antigravity_response.go:108`,
  `codex/openai/responses/codex_openai-responses_response.go:14`,
  `gemini/openai/responses/gemini_openai-responses_response.go:161`.
- Bug: `bytes.HasPrefix(trimmed, []byte("data:"))` + `trimmed[len("data:"):]`
  accepts `data:payload` without the SSE-mandated space and any substring start.
- Intended fix: add a shared helper in `internal/translator/common` that requires
  `data:` optionally followed by exactly one space, and use it everywhere:
  ```go
  func SSEDataPayload(line []byte) ([]byte, bool) {
      line = bytes.TrimSpace(line)
      if !bytes.HasPrefix(line, []byte("data:")) { return nil, false }
      rest := line[len("data:"):]
      if len(rest) > 0 && rest[0] == ' ' { rest = rest[1:] }
      return bytes.TrimSpace(rest), true
  }
  ```

### #29 — extract duplicated `ToolCallAccumulator` to `common`
- Files: identical struct defined in `openai/claude/openai_claude_response.go:71`,
  `openai/gemini/openai_gemini_response.go:31`,
  `claude/openai/chat-completions/claude_openai_response.go:42`.
- Bug: three copies of the same type.
- Intended fix: define `ToolCallAccumulator` once in
  `internal/translator/common` and import it in the three packages.

### #30 — name interactions magic numbers as constants
- Files: `interactions/claude/interactions_claude_response.go`,
  `antigravity/interactions/interactions_antigravity_response.go` (event templates
  using bare `0` for `sequence_number` / `output_index` / `summary_index`).
- Bug: repeated magic `0` literals obscure intent and invite off-by-one drift.
- Intended fix: introduce named constants (e.g. `defaultSequenceNumber = 0`,
  `firstOutputIndex = 0`, `firstSummaryIndex = 0`) and use them in the templates.

### #31 — Gemini empty-body passthrough
- File: Gemini response translator(s) (`gemini/...`) where an empty upstream body
  is forwarded verbatim.
- Bug: an empty/malformed upstream body is passed through instead of being
  normalized to a valid empty completion/structure.
- Intended fix: detect empty body and emit a valid empty response envelope
  (no content, `stop` reason, zero usage) rather than forwarding raw empty bytes.

## Verification
- `go build ./internal/translator/...`
- `go test ./internal/translator/...` (including the existing
  `TestConvertCodexResponseToClaude_StreamDeferredUnnamedFunctionCallDoesNotReserveBlockIndex`
  and the `fixCLIToolResponse` cases in `antigravity_gemini_request_test.go`).
- `gofmt -l internal/translator/...` clean; `go vet ./internal/translator/...` clean.
