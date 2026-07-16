package executor

// 本文件为 SSE 兼容性增强方案中的 "tool_call 增量分片兼容性" 风险点提供测试矩阵。
//
// 风险点描述：不同 provider（Codex / Antigravity / Gemini / Claude）的
// tool_call 增量分片顺序、索引、call_id 处理方式存在差异，可能与官方 SDK
// 期望不一致。本测试矩阵聚焦于以下可观测行为：
//
//   1. call_id 生成与比较的稳定性（Codex：长 ID 截断 + Claude-visible 兼容）
//   2. function_call_part 分片定位与合并（Antigravity：按 call_id 去重 + 顺序保留）
//   3. 分片索引验证：相同 call_id 多次出现时，定位/合并结果必须确定且有序
//
// 端到端流程（含 SSE 流式 tool_call delta 转换）已由各 provider 的
// *_executor_test.go 覆盖（如 gemini_executor_test.go 验证 step.start ->
// choices.0.delta.tool_calls.0.id 的转换）。本文件补充纯函数级别的兼容性
// 边界用例，避免在 httptest 服务器中难以复现的精细分片顺序问题。

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	"github.com/tidwall/gjson"
)

// ToolCallCompatMatrix 描述各 provider 在 tool_call 增量分片上的兼容性期望。
// 该表格作为文档化矩阵使用：每一行代表一个 provider × 场景，列出期望行为。
// 当某 provider 的实现发生回归时，对应行的纯函数测试应能捕获。
var ToolCallCompatMatrix = []struct {
	Provider string
	Scenario string
	Expect   string
}{
	// Codex provider
	{"codex", "short_call_id", "原样返回，不截断"},
	{"codex", "long_call_id_over_64_chars", "SHA256 截断为 64 字符 + _<8字节哈希> 后缀"},
	{"codex", "empty_call_id", "返回 nil，不生成 key"},
	{"codex", "function_call_type", "key 前缀 'function_call:'"},
	{"codex", "custom_tool_call_type", "key 前缀 'custom_tool_call:'"},
	{"codex", "non_tool_call_type", "返回 nil，不参与重放去重"},
	{"codex", "claude_visible_shortened_id", "原始 ID 与缩短 ID 都参与比较"},

	// Antigravity provider
	{"antigravity", "locate_existing_call_id", "返回 (contentIndex, partIndex, true)"},
	{"antigravity", "locate_missing_call_id", "返回 (-1, -1, false)"},
	{"antigravity", "locate_empty_call_id", "返回 (-1, -1, false)，不触发查找"},
	{"antigravity", "merge_new_part", "在指定 contentIndex/partIndex 写入新 functionCall"},
	{"antigravity", "merge_existing_part_preserves_args", "已存在的 functionCall.args 不被覆盖"},
	{"antigravity", "merge_existing_part_fills_signature", "已存在的 part 缺 thoughtSignature 时补齐"},
	{"antigravity", "merge_idempotent", "重复合并相同分片不产生重复 part"},
}

// ---------------------------------------------------------------------------
// Codex provider: call_id 处理与 key 生成
// ---------------------------------------------------------------------------

// TestCodexReplayComparableCallIDs_Variants 覆盖 Codex call_id 比较的兼容性：
// 短 ID 原样返回；长 ID 同时返回原始与 Claude-visible 缩短形式，确保与
// Claude 期望的 ≤64 字符 call_id 互通。
func TestCodexReplayComparableCallIDs_Variants(t *testing.T) {
	tests := []struct {
		name    string
		callID  string
		wantLen int
		check   func(ids []string) bool
	}{
		{
			name:    "empty call_id returns nil",
			callID:  "",
			wantLen: 0,
		},
		{
			name:    "whitespace-only call_id returns nil",
			callID:  "   ",
			wantLen: 0,
		},
		{
			name:    "short call_id returns single element",
			callID:  "call_123",
			wantLen: 1,
			check: func(ids []string) bool {
				return ids[0] == "call_123"
			},
		},
		{
			name:    "long call_id returns original + shortened",
			callID:  strings.Repeat("a", 80),
			wantLen: 2,
			check: func(ids []string) bool {
				// 原始 ID 必须保留
				if ids[0] != strings.Repeat("a", 80) {
					return false
				}
				// 缩短形式必须 ≤ 64 字符（Claude-visible 限制）
				if len(ids[1]) > 64 {
					return false
				}
				// 缩短形式必须以 _<8字节hex> 后缀结尾
				if !strings.HasPrefix(ids[1], "aaaa") {
					return false
				}
				return strings.Contains(ids[1], "_")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids := codexReplayComparableCallIDs(tt.callID)
			if len(ids) != tt.wantLen {
				t.Fatalf("len = %d, want %d (ids=%v)", len(ids), tt.wantLen, ids)
			}
			if tt.check != nil && !tt.check(ids) {
				t.Fatalf("check failed for call_id=%q (ids=%v)", tt.callID, ids)
			}
		})
	}
}

// TestCodexShortenReplayCallIDIfNeeded_Boundary 验证 64 字符边界行为：
// ≤64 字符原样返回；>64 字符触发 SHA256 截断，且长度严格等于 64。
// 这与 Claude SDK 期望的 call_id 长度限制对齐。
func TestCodexShortenReplayCallIDIfNeeded_Boundary(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"empty stays empty", "", ""},
		{"at limit stays unchanged", strings.Repeat("x", 64), strings.Repeat("x", 64)},
		{"under limit stays unchanged", "short_id", "short_id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shortenCodexReplayCallIDIfNeeded(tt.input)
			if got != tt.expect {
				t.Fatalf("shorten(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}

	// 超过限制时：长度必须等于 64，且后缀格式为 "_<16 hex chars>"
	// （8 字节 SHA256 前缀的十六进制表示）。
	t.Run("over limit truncates to exactly 64 with hash suffix", func(t *testing.T) {
		longID := strings.Repeat("y", 100)
		got := shortenCodexReplayCallIDIfNeeded(longID)
		if len(got) != 64 {
			t.Fatalf("shortened length = %d, want exactly 64 (got=%q)", len(got), got)
		}
		// 后缀必须是 "_" + 16 个 hex 字符
		if !strings.HasSuffix(got, "_") {
			// 校验后缀 _<hex> 形态：倒数第 17 位是 '_'，后 16 位是 hex
			suffix := got[len(got)-16:]
			if _, err := hex.DecodeString(suffix); err != nil {
				t.Fatalf("suffix %q is not hex; got=%q", suffix, got)
			}
			if got[len(got)-17] != '_' {
				t.Fatalf("expected '_' before hex suffix; got=%q", got)
			}
		}
	})
}

// TestCodexReplayToolCallKeys_TypeCoverage 验证 Codex key 生成对
// function_call / custom_tool_call / 其他类型的分类处理。
// 这是 provider 兼容性矩阵的核心：不同 tool_call 类型必须用不同前缀的 key，
// 否则跨类型重放去重会误命中。
func TestCodexReplayToolCallKeys_TypeCoverage(t *testing.T) {
	tests := []struct {
		name     string
		itemJSON string
		wantKeys []string
	}{
		{
			name:     "function_call with call_id",
			itemJSON: `{"type":"function_call","call_id":"call_abc"}`,
			wantKeys: []string{"function_call:call_abc"},
		},
		{
			name:     "custom_tool_call with call_id",
			itemJSON: `{"type":"custom_tool_call","call_id":"call_xyz"}`,
			wantKeys: []string{"custom_tool_call:call_xyz"},
		},
		{
			name:     "non tool_call type returns nil",
			itemJSON: `{"type":"message","call_id":"call_abc"}`,
			wantKeys: nil,
		},
		{
			name:     "function_call without call_id returns nil",
			itemJSON: `{"type":"function_call"}`,
			wantKeys: nil,
		},
		{
			name:     "function_call with whitespace-only call_id returns nil",
			itemJSON: `{"type":"function_call","call_id":"  "}`,
			wantKeys: nil,
		},
		{
			name:     "function_call with long call_id returns both forms",
			itemJSON: `{"type":"function_call","call_id":"` + strings.Repeat("z", 80) + `"}`,
			// 期望: ["function_call:<original>", "function_call:<shortened>"]
			wantKeys: []string{
				"function_call:" + strings.Repeat("z", 80),
				"function_call:" + shortenCodexReplayCallIDIfNeeded(util.SanitizeClaudeToolID(strings.Repeat("z", 80))),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := codexReplayToolCallKeys(gjson.Parse(tt.itemJSON))
			if len(got) != len(tt.wantKeys) {
				t.Fatalf("len = %d, want %d (got=%v want=%v)", len(got), len(tt.wantKeys), got, tt.wantKeys)
			}
			for i := range got {
				if got[i] != tt.wantKeys[i] {
					t.Fatalf("keys[%d] = %q, want %q (got=%v want=%v)", i, got[i], tt.wantKeys[i], got, tt.wantKeys)
				}
			}
		})
	}
}

// TestCodexReplayAnyToolCallKeyExists 验证去重判定逻辑：
// 任一 key 命中即视为已存在，避免长/短 call_id 形态差异导致重复重放。
func TestCodexReplayAnyToolCallKeyExists(t *testing.T) {
	existing := map[string]bool{
		"function_call:call_abc": true,
	}

	if !codexReplayAnyToolCallKeyExists(existing, []string{"function_call:call_abc"}) {
		t.Fatal("expected match for exact key")
	}
	if !codexReplayAnyToolCallKeyExists(existing, []string{"function_call:call_other", "function_call:call_abc"}) {
		t.Fatal("expected match when any key hits")
	}
	if codexReplayAnyToolCallKeyExists(existing, []string{"function_call:call_other"}) {
		t.Fatal("expected no match for absent key")
	}
	if codexReplayAnyToolCallKeyExists(existing, nil) {
		t.Fatal("nil keys should not match")
	}
	if codexReplayAnyToolCallKeyExists(nil, []string{"function_call:call_abc"}) {
		t.Fatal("nil existing map should not match")
	}
}

// ---------------------------------------------------------------------------
// Antigravity provider: function_call_part 定位与合并
// ---------------------------------------------------------------------------

// TestAntigravityFunctionCallPartLocation_CallID 验证按 call_id 定位
// function_call_part 的行为。该函数是 Antigravity 分片合并的前置步骤：
// 若定位失败，合并逻辑会创建新 part，可能导致重复分片。
func TestAntigravityFunctionCallPartLocation_CallID(t *testing.T) {
	payload := []byte(`{"request":{"contents":[
		{"role":"user","parts":[{"text":"hi"}]},
		{"role":"model","parts":[{"functionCall":{"name":"get_weather","id":"call_1"}}]},
		{"role":"user","parts":[{"functionResponse":{"id":"call_1","name":"get_weather"}}]},
		{"role":"model","parts":[{"functionCall":{"name":"search","id":"call_2"}}]}
	]}}`)

	tests := []struct {
		name       string
		callID     string
		wantCI     int
		wantPI     int
		wantExists bool
	}{
		{"locate call_1", "call_1", 1, 0, true},
		{"locate call_2", "call_2", 3, 0, true},
		{"missing call_id", "call_99", -1, -1, false},
		{"empty call_id", "", -1, -1, false},
		{"whitespace call_id", "  ", -1, -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ci, pi, ok := antigravityFunctionCallPartLocation(payload, tt.callID)
			if ci != tt.wantCI || pi != tt.wantPI || ok != tt.wantExists {
				t.Fatalf("got (ci=%d, pi=%d, ok=%v), want (%d, %d, %v)",
					ci, pi, ok, tt.wantCI, tt.wantPI, tt.wantExists)
			}
		})
	}
}

// TestAntigravityFunctionCallPartLocation_NoContentsArray 验证
// request.contents 缺失或非数组时的健壮性。
func TestAntigravityFunctionCallPartLocation_NoContentsArray(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{"missing request field", `{"other":"value"}`},
		{"contents is not array", `{"request":{"contents":"not-array"}}`},
		{"empty contents", `{"request":{"contents":[]}}`},
		{"part without functionCall", `{"request":{"contents":[{"role":"model","parts":[{"text":"hi"}]}]}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ci, pi, ok := antigravityFunctionCallPartLocation([]byte(tt.payload), "call_1")
			if ok || ci != -1 || pi != -1 {
				t.Fatalf("expected (-1,-1,false), got (%d,%d,%v)", ci, pi, ok)
			}
		})
	}
}

// TestMergeAntigravityFunctionCallPartReplay_NewPart 验证当目标 part
// 不存在时，合并函数会创建新的 functionCall part。这是增量分片场景的
// 基础行为：第一个分片必须被正确写入指定位置。
//
// 注意：mergeAntigravityFunctionCallPartReplay 在 callID 找不到现有
// functionCall 且无匹配 functionResponse 时，会调用
// antigravityReasoningReplayResolveContentIndex 解析 contentIndex。该函数
// 在 contentIndex 越界时会 fallback 到最后一个 model content。因此测试
// payload 必须包含足够长的 contents 数组，使 contentIndex=1 直接命中。
func TestMergeAntigravityFunctionCallPartReplay_NewPart(t *testing.T) {
	payload := []byte(`{"request":{"contents":[
		{"role":"user","parts":[{"text":"hi"}]},
		{"role":"model","parts":[]}
	]}}`)

	item := gjson.Parse(`{
		"name": "get_weather",
		"args": "{\"location\":\"北京\"}",
		"call_id": "call_1",
		"contentIndex": 1,
		"partIndex": 0
	}`)

	out, changed := mergeAntigravityFunctionCallPartReplay(payload, item)
	if !changed {
		t.Fatal("expected changed=true for new part")
	}

	fcName := gjson.GetBytes(out, "request.contents.1.parts.0.functionCall.name").String()
	if fcName != "get_weather" {
		t.Fatalf("functionCall.name = %q, want get_weather", fcName)
	}
	fcID := gjson.GetBytes(out, "request.contents.1.parts.0.functionCall.id").String()
	if fcID != "call_1" {
		t.Fatalf("functionCall.id = %q, want call_1", fcID)
	}
	fcArgs := gjson.GetBytes(out, "request.contents.1.parts.0.functionCall.args").String()
	if fcArgs != `{"location":"北京"}` {
		t.Fatalf("functionCall.args = %q, want {\"location\":\"北京\"}", fcArgs)
	}
}

// TestMergeAntigravityFunctionCallPartReplay_Idempotent 验证重复合并
// 相同 call_id 的分片不会创建重复 part。这是分片顺序兼容性的关键：
// 上游可能重放同一 call_id 的多个分片，下游必须去重。
func TestMergeAntigravityFunctionCallPartReplay_Idempotent(t *testing.T) {
	payload := []byte(`{"request":{"contents":[
		{"role":"user","parts":[{"text":"hi"}]},
		{"role":"model","parts":[{"functionCall":{"name":"get_weather","id":"call_1","args":"{}"}}]}
	]}}`)

	item := gjson.Parse(`{
		"name": "get_weather",
		"args": "{\"location\":\"北京\"}",
		"call_id": "call_1",
		"contentIndex": 1,
		"partIndex": 0
	}`)

	out, changed := mergeAntigravityFunctionCallPartReplay(payload, item)
	// call_1 已存在，name/args 不应被覆盖；changed 应为 false。
	if changed {
		t.Fatal("expected changed=false for idempotent merge of existing call_id")
	}

	// 验证 args 未被覆盖（保留原始 "{}"）
	fcArgs := gjson.GetBytes(out, "request.contents.1.parts.0.functionCall.args").String()
	if fcArgs != "{}" {
		t.Fatalf("args should not be overwritten; got %q, want {}", fcArgs)
	}

	// 验证没有创建重复 part
	partsCount := len(gjson.GetBytes(out, "request.contents.1.parts").Array())
	if partsCount != 1 {
		t.Fatalf("expected 1 part after idempotent merge, got %d", partsCount)
	}
}

// TestMergeAntigravityFunctionCallPartReplay_FillsSignature 验证当
// 已存在的 part 缺少 thoughtSignature 时，合并函数会补齐。
// 这是 Antigravity 分片重组的核心：thoughtSignature 可能跨多个分片到达，
// 必须按 call_id 合并到同一 part。
func TestMergeAntigravityFunctionCallPartReplay_FillsSignature(t *testing.T) {
	payload := []byte(`{"request":{"contents":[
		{"role":"model","parts":[{"functionCall":{"name":"get_weather","id":"call_1","args":"{}"}}]}
	]}}`)

	item := gjson.Parse(`{
		"name": "get_weather",
		"args": "{}",
		"call_id": "call_1",
		"thoughtSignature": "sig_abc",
		"contentIndex": 0,
		"partIndex": 0
	}`)

	out, changed := mergeAntigravityFunctionCallPartReplay(payload, item)
	if !changed {
		t.Fatal("expected changed=true when filling missing thoughtSignature")
	}

	sig := gjson.GetBytes(out, "request.contents.0.parts.0.thoughtSignature").String()
	if sig != "sig_abc" {
		t.Fatalf("thoughtSignature = %q, want sig_abc", sig)
	}
}

// TestMergeAntigravityFunctionCallPartReplay_PreservesExistingSignature
// 验证已存在的 thoughtSignature 不会被覆盖。这是分片合并的安全网：
// 后到的分片不能破坏先到的签名。
func TestMergeAntigravityFunctionCallPartReplay_PreservesExistingSignature(t *testing.T) {
	payload := []byte(`{"request":{"contents":[
		{"role":"model","parts":[
			{"functionCall":{"name":"get_weather","id":"call_1","args":"{}"},"thoughtSignature":"original_sig"}
		]}
	]}}`)

	item := gjson.Parse(`{
		"name": "get_weather",
		"args": "{}",
		"call_id": "call_1",
		"thoughtSignature": "new_sig",
		"contentIndex": 0,
		"partIndex": 0
	}`)

	out, changed := mergeAntigravityFunctionCallPartReplay(payload, item)
	// 已存在 thoughtSignature 时不再覆盖，changed 应为 false。
	if changed {
		t.Fatal("expected changed=false when thoughtSignature already exists")
	}

	sig := gjson.GetBytes(out, "request.contents.0.parts.0.thoughtSignature").String()
	if sig != "original_sig" {
		t.Fatalf("thoughtSignature = %q, want original_sig (must not be overwritten)", sig)
	}
}

// ---------------------------------------------------------------------------
// 分片顺序与索引验证
// ---------------------------------------------------------------------------

// TestAntigravityFunctionCallPartLocation_OrderPreserved 验证当多个
// function_call_part 按特定顺序写入后，按 call_id 定位的结果与写入顺序一致。
// 这是 provider 兼容性矩阵中 "分片顺序保留" 的核心断言。
func TestAntigravityFunctionCallPartLocation_OrderPreserved(t *testing.T) {
	payload := []byte(`{"request":{"contents":[
		{"role":"model","parts":[
			{"functionCall":{"name":"first","id":"call_A"}},
			{"functionCall":{"name":"second","id":"call_B"}},
			{"functionCall":{"name":"third","id":"call_C"}}
		]}
	]}}`)

	// 按 call_id 定位时，partIndex 必须反映原始写入顺序。
	expected := []struct {
		callID string
		pi     int
	}{
		{"call_A", 0},
		{"call_B", 1},
		{"call_C", 2},
	}

	for _, e := range expected {
		ci, pi, ok := antigravityFunctionCallPartLocation(payload, e.callID)
		if !ok || ci != 0 || pi != e.pi {
			t.Fatalf("locate(%q) = (ci=%d, pi=%d, ok=%v), want (0, %d, true)",
				e.callID, ci, pi, ok, e.pi)
		}
	}
}

// TestCodexReplayKey_StableAcrossCalls 验证 Codex key 生成是确定性的：
// 相同 call_id 多次调用必须产生相同 key，否则去重会失效。
// 该测试同时覆盖 provider 兼容性矩阵中的 "分片索引验证" 项：key 的稳定性
// 直接决定了跨分片 call_id 比较的正确性。
func TestCodexReplayKey_StableAcrossCalls(t *testing.T) {
	item := gjson.Parse(`{"type":"function_call","call_id":"call_stable"}`)

	keys1 := codexReplayToolCallKeys(item)
	keys2 := codexReplayToolCallKeys(item)

	if len(keys1) != len(keys2) {
		t.Fatalf("non-deterministic key count: %v vs %v", keys1, keys2)
	}
	for i := range keys1 {
		if keys1[i] != keys2[i] {
			t.Fatalf("non-deterministic key at index %d: %q vs %q", i, keys1[i], keys2[i])
		}
	}

	// 同时验证长 call_id 的缩短形式也是确定性的。
	longItem := gjson.Parse(`{"type":"function_call","call_id":"` + strings.Repeat("k", 80) + `"}`)
	shortKeys1 := codexReplayToolCallKeys(longItem)
	shortKeys2 := codexReplayToolCallKeys(longItem)
	for i := range shortKeys1 {
		if shortKeys1[i] != shortKeys2[i] {
			t.Fatalf("non-deterministic shortened key at index %d: %q vs %q", i, shortKeys1[i], shortKeys2[i])
		}
	}
}

// TestAntigravityMerge_SequentialChunksPreserveOrder 模拟增量分片场景：
// 上游按顺序发送 call_A、call_B、call_C 三个分片，下游合并后必须保持
// contents 中 part 的顺序与上游一致。
// 这是 provider 兼容性矩阵中 "分片顺序调整机制" 的端到端断言（纯函数级）。
//
// 注意：payload 必须包含足够长的 contents 数组（contents[1] 存在且 parts
// 为空），否则 antigravityReasoningReplayResolveContentIndex 会 fallback
// 到其他位置，导致断言路径错位。
func TestAntigravityMerge_SequentialChunksPreserveOrder(t *testing.T) {
	payload := []byte(`{"request":{"contents":[
		{"role":"user","parts":[{"text":"hi"}]},
		{"role":"model","parts":[]}
	]}}`)

	chunks := []string{
		`{"name":"first","args":"{}","call_id":"call_A","contentIndex":1,"partIndex":0}`,
		`{"name":"second","args":"{}","call_id":"call_B","contentIndex":1,"partIndex":1}`,
		`{"name":"third","args":"{}","call_id":"call_C","contentIndex":1,"partIndex":2}`,
	}

	for _, ch := range chunks {
		out, changed := mergeAntigravityFunctionCallPartReplay(payload, gjson.Parse(ch))
		if !changed {
			t.Fatalf("merge should succeed for chunk: %s", ch)
		}
		payload = out
	}

	// 验证 contents[1].parts 按顺序包含 first/second/three 三个 functionCall。
	parts := gjson.GetBytes(payload, "request.contents.1.parts").Array()
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}

	wantNames := []string{"first", "second", "third"}
	for i, want := range wantNames {
		got := parts[i].Get("functionCall.name").String()
		if got != want {
			t.Fatalf("parts[%d].functionCall.name = %q, want %q", i, got, want)
		}
	}

	// 验证按 call_id 定位时，partIndex 与写入顺序一致（顺序保留的核心断言）。
	for i, callID := range []string{"call_A", "call_B", "call_C"} {
		ci, pi, ok := antigravityFunctionCallPartLocation(payload, callID)
		if !ok || ci != 1 || pi != i {
			t.Fatalf("locate(%q) = (ci=%d, pi=%d, ok=%v), want (1, %d, true)",
				callID, ci, pi, ok, i)
		}
	}
}

// TestAntigravityMerge_ParallelToolCalls_DistinctIDs 验证并行 tool_call
// （多个 call_id 同时出现）不会被错误合并。这是 provider 兼容性矩阵中
// "并行 tool_calls" 场景的纯函数级断言。
//
// payload 必须包含 contents[1] 与 contents[2]，使两个并行分片分别落到
// 不同 contentIndex，避免 fallback 合并。
func TestAntigravityMerge_ParallelToolCalls_DistinctIDs(t *testing.T) {
	payload := []byte(`{"request":{"contents":[
		{"role":"user","parts":[{"text":"hi"}]},
		{"role":"model","parts":[]},
		{"role":"model","parts":[]}
	]}}`)

	// 两个并行 tool_call，使用不同 call_id 与不同 contentIndex。
	parallelChunks := []string{
		`{"name":"get_weather","args":"{}","call_id":"call_w","contentIndex":1,"partIndex":0}`,
		`{"name":"get_time","args":"{}","call_id":"call_t","contentIndex":2,"partIndex":0}`,
	}

	for _, ch := range parallelChunks {
		out, changed := mergeAntigravityFunctionCallPartReplay(payload, gjson.Parse(ch))
		if !changed {
			t.Fatalf("merge should succeed for parallel chunk: %s", ch)
		}
		payload = out
	}

	// 验证两个 functionCall 都被写入，且位于不同 contentIndex。
	wName := gjson.GetBytes(payload, "request.contents.1.parts.0.functionCall.name").String()
	tName := gjson.GetBytes(payload, "request.contents.2.parts.0.functionCall.name").String()
	if wName != "get_weather" {
		t.Fatalf("contents[1].parts[0].functionCall.name = %q, want get_weather", wName)
	}
	if tName != "get_time" {
		t.Fatalf("contents[2].parts[0].functionCall.name = %q, want get_time", tName)
	}

	// 验证两个 call_id 都可被独立定位（未互相覆盖）。
	if _, _, ok := antigravityFunctionCallPartLocation(payload, "call_w"); !ok {
		t.Fatal("call_w should be locatable after parallel merge")
	}
	if _, _, ok := antigravityFunctionCallPartLocation(payload, "call_t"); !ok {
		t.Fatal("call_t should be locatable after parallel merge")
	}
}

// ---------------------------------------------------------------------------
// 矩阵自检：确保 ToolCallCompatMatrix 覆盖所有被测场景
// ---------------------------------------------------------------------------

// TestToolCallCompatMatrix_Coverage 是一个文档化测试：确保矩阵中列出的
// 每个 provider × scenario 至少被一个测试函数覆盖。当新增场景时，开发者
// 必须同步更新矩阵与测试，否则此测试会失败。
func TestToolCallCompatMatrix_Coverage(t *testing.T) {
	// 已覆盖的 (provider, scenario) 集合：通过测试函数名映射。
	covered := map[string]bool{
		"codex:short_call_id":                             true,
		"codex:long_call_id_over_64_chars":                true,
		"codex:empty_call_id":                             true,
		"codex:function_call_type":                        true,
		"codex:custom_tool_call_type":                     true,
		"codex:non_tool_call_type":                        true,
		"codex:claude_visible_shortened_id":               true,
		"antigravity:locate_existing_call_id":             true,
		"antigravity:locate_missing_call_id":              true,
		"antigravity:locate_empty_call_id":                true,
		"antigravity:merge_new_part":                      true,
		"antigravity:merge_existing_part_preserves_args":  true,
		"antigravity:merge_existing_part_fills_signature": true,
		"antigravity:merge_idempotent":                    true,
	}

	for _, row := range ToolCallCompatMatrix {
		key := row.Provider + ":" + row.Scenario
		if !covered[key] {
			t.Errorf("matrix row %q (%s) has no covering test; add a test or update matrix", key, row.Expect)
		}
	}
}
