# Agent Collaboration Notes

This document records unexpected situations and pitfalls encountered during
development, to help subsequent AI agents avoid stepping into the same traps.
Each entry follows the format: `### [Category] Problem Description`, followed
by detailed description, impact scope, and suggested solutions.

---

## Active Pitfalls

### [MiMo Integration] Multi-turn tool calls must pass reasoning_content back

**Problem**

MiMo official docs require follow-up assistant messages with `tool_calls` to
include the full `reasoning_content` when thinking mode is enabled. Otherwise,
the API returns 400. This matches Kimi behavior.

**Impact**

If MiMo routes through `openai_compat_executor.go` without request
normalization, multi-turn tool calls can fail for agent clients.

**Suggested fix**

Add MiMo `reasoning_content` pass-back normalization, either in a dedicated
MiMo executor wrapper or conditionally in the OpenAI-compatible executor when
`modelkind.IsMIMOModel(baseModel)`.

### [MiMo Integration] temperature/top_p are locked in thinking mode

**Problem**

MiMo docs state that thinking-mode models such as `mimo-v2.5-pro` and
`mimo-v2.5` do not support custom `temperature` and `top_p`; recommended values
are `1.0` and `0.95`.

**Impact**

Requests with custom sampling values may be rejected or behave unexpectedly.

**Suggested fix**

If this is confirmed to cause 400s, add `mimoLockThinkingParams` in a MiMo
executor wrapper or conditionally after `ApplyPayloadConfigWithRequest`.

### [DeepSeek Integration] Tool-call follow-up turns must pass reasoning_content back

**Problem**

DeepSeek docs require full `reasoning_content` on assistant messages that
contain `tool_calls` in thinking mode follow-up requests. Missing content can
return 400.

**Impact**

DeepSeek thinking-mode multi-turn tool calls can fail when agent clients omit
`reasoning_content`.

**Suggested fix**

Keep DeepSeek request normalization in place after payload config application,
using fallback order: current `reasoning`, current `content`, most recent
existing `reasoning_content`, then `"[reasoning unavailable]"`.

### [DeepSeek KV Cache] Usage hit fields need cached_tokens normalization

**Problem**

DeepSeek reports cache hits via `usage.prompt_cache_hit_tokens` and
`usage.prompt_cache_miss_tokens`, while cross-protocol usage paths mostly read
`prompt_tokens_details.cached_tokens` or `input_tokens_details.cached_tokens`.

**Impact**

KV cache hit data can be lost in usage accounting or protocol translation.

**Suggested fix**

Parse DeepSeek hit fields as cached tokens and normalize responses before
translation while preserving original DeepSeek fields. If cache hit rate is
still low, check `routing.session-affinity`.

### [Thinking Validation] isOpenAIFamily must not include xai

**Problem**

`internal/thinking/validate.go` uses `isOpenAIFamily` to decide whether level
and budget validation should be strict. Adding `xai` would make `openai -> xai`
look like same-family translation and break cross-family level clamping.

**Suggested fix**

Do not add `xai` to `isOpenAIFamily`; current OpenAI-family list should remain
`openai`, `openai-response`, `codex`, `deepseek`, and `mimo`.
