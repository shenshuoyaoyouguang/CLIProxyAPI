package reasoning

import (
	"encoding/base64"
	"testing"

	sigcompat "github.com/router-for-me/CLIProxyAPI/v7/internal/signature"
	"github.com/tidwall/gjson"
)

// validGPTReasoningSignature builds a syntactically valid GPT reasoning
// signature: a Fernet-like base64url payload starting with "gAAAA".
func validGPTReasoningSignature() string {
	payload := make([]byte, 1+8+16+16+32)
	payload[0] = 0x80
	for i := 9; i < len(payload); i++ {
		payload[i] = byte(i)
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

// validGPTReasoningSignatureVariant builds a different valid GPT reasoning
// signature, using XOR'd byte fill and configurable ciphertext size (via
// sizeSeed: <=16 → 16B, <=32 → 32B, else 64B) so variants differ from the
// default signature and from each other.
func validGPTReasoningSignatureVariant(sizeSeed int) string {
	var size int
	switch {
	case sizeSeed <= 16:
		size = 16
	case sizeSeed <= 32:
		size = 32
	default:
		size = 64
	}
	payload := make([]byte, 1+8+16+size+32)
	payload[0] = 0x80
	for i := 9; i < len(payload); i++ {
		payload[i] = byte(i ^ 0xff)
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

// ---------------------------------------------------------------------------
// CollectOpenAIReasoningParts
// ---------------------------------------------------------------------------

func TestCollectOpenAIReasoningParts_NonExistentNode(t *testing.T) {
	node := gjson.Get("{}", "nonexistent")
	opts := OpenAIReasoningOptions{}
	parts := CollectOpenAIReasoningParts(node, opts)
	if len(parts) != 0 {
		t.Fatalf("expected 0 parts for non-existent node, got %d", len(parts))
	}
}

func TestCollectOpenAIReasoningParts_EmptyString(t *testing.T) {
	node := gjson.Parse(`""`)
	opts := OpenAIReasoningOptions{}
	parts := CollectOpenAIReasoningParts(node, opts)
	if len(parts) != 0 {
		t.Fatalf("expected 0 parts for empty string, got %d", len(parts))
	}
}

func TestCollectOpenAIReasoningParts_PlainString(t *testing.T) {
	node := gjson.Parse(`"Let me think step by step"`)
	opts := OpenAIReasoningOptions{}
	parts := CollectOpenAIReasoningParts(node, opts)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].Text != "Let me think step by step" {
		t.Fatalf("expected text %q, got %q", "Let me think step by step", parts[0].Text)
	}
	if parts[0].Signature != "" {
		t.Fatalf("expected empty signature for plain string, got %q", parts[0].Signature)
	}
}

func TestCollectOpenAIReasoningParts_StringArray(t *testing.T) {
	node := gjson.Parse(`["first thought", "second thought", "third thought"]`)
	opts := OpenAIReasoningOptions{}
	parts := CollectOpenAIReasoningParts(node, opts)
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}
	if parts[0].Text != "first thought" {
		t.Fatalf("parts[0].Text = %q, want %q", parts[0].Text, "first thought")
	}
	if parts[1].Text != "second thought" {
		t.Fatalf("parts[1].Text = %q, want %q", parts[1].Text, "second thought")
	}
	if parts[2].Text != "third thought" {
		t.Fatalf("parts[2].Text = %q, want %q", parts[2].Text, "third thought")
	}
}

func TestCollectOpenAIReasoningParts_MixedArray(t *testing.T) {
	node := gjson.Parse(`["plain text", {"text": "object text", "signature": "gpt#` + validGPTReasoningSignature() + `"}]`)
	opts := OpenAIReasoningOptions{
		TargetProvider:     sigcompat.SignatureProviderGPT,
		SignatureBlockKind: sigcompat.SignatureBlockKindGPTReasoning,
	}
	parts := CollectOpenAIReasoningParts(node, opts)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0].Text != "plain text" {
		t.Fatalf("parts[0].Text = %q, want %q", parts[0].Text, "plain text")
	}
	if parts[1].Text != "object text" {
		t.Fatalf("parts[1].Text = %q, want %q", parts[1].Text, "object text")
	}
	if parts[1].Signature == "" {
		t.Fatal("expected non-empty signature for GPT-compatible object")
	}
}

func TestCollectOpenAIReasoningParts_NestedArray(t *testing.T) {
	node := gjson.Parse(`[["nested deep"]]`)
	opts := OpenAIReasoningOptions{}
	parts := CollectOpenAIReasoningParts(node, opts)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part from nested array, got %d", len(parts))
	}
	if parts[0].Text != "nested deep" {
		t.Fatalf("parts[0].Text = %q, want %q", parts[0].Text, "nested deep")
	}
}

func TestCollectOpenAIReasoningParts_EmptyArray(t *testing.T) {
	node := gjson.Parse(`[]`)
	opts := OpenAIReasoningOptions{}
	parts := CollectOpenAIReasoningParts(node, opts)
	if len(parts) != 0 {
		t.Fatalf("expected 0 parts for empty array, got %d", len(parts))
	}
}

func TestCollectOpenAIReasoningParts_ObjectWithText(t *testing.T) {
	node := gjson.Parse(`{"text": "reasoning text"}`)
	opts := OpenAIReasoningOptions{}
	parts := CollectOpenAIReasoningParts(node, opts)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].Text != "reasoning text" {
		t.Fatalf("parts[0].Text = %q, want %q", parts[0].Text, "reasoning text")
	}
}

func TestCollectOpenAIReasoningParts_ObjectWithThinking(t *testing.T) {
	node := gjson.Parse(`{"thinking": "thinking text"}`)
	opts := OpenAIReasoningOptions{}
	parts := CollectOpenAIReasoningParts(node, opts)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].Text != "thinking text" {
		t.Fatalf("parts[0].Text = %q, want %q", parts[0].Text, "thinking text")
	}
}

func TestCollectOpenAIReasoningParts_ObjectWithContent(t *testing.T) {
	node := gjson.Parse(`{"content": "content text"}`)
	opts := OpenAIReasoningOptions{}
	parts := CollectOpenAIReasoningParts(node, opts)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].Text != "content text" {
		t.Fatalf("parts[0].Text = %q, want %q", parts[0].Text, "content text")
	}
}

func TestCollectOpenAIReasoningParts_TextFirstWins(t *testing.T) {
	// FirstStringValue checks in order: text, thinking, content.
	node := gjson.Parse(`{"content": "fallback", "text": "winner"}`)
	opts := OpenAIReasoningOptions{}
	parts := CollectOpenAIReasoningParts(node, opts)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].Text != "winner" {
		t.Fatalf("parts[0].Text = %q, want %q", parts[0].Text, "winner")
	}
}

func TestCollectOpenAIReasoningParts_ObjectWithTextAndCompatibleSignature(t *testing.T) {
	gptSig := validGPTReasoningSignature()
	node := gjson.Parse(`{"text": "reasoning text", "signature": "gpt#` + gptSig + `"}`)
	opts := OpenAIReasoningOptions{
		TargetProvider:     sigcompat.SignatureProviderGPT,
		SignatureBlockKind: sigcompat.SignatureBlockKindGPTReasoning,
	}
	parts := CollectOpenAIReasoningParts(node, opts)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].Text != "reasoning text" {
		t.Fatalf("parts[0].Text = %q, want %q", parts[0].Text, "reasoning text")
	}
	if parts[0].Signature == "" {
		t.Fatal("expected non-empty signature for GPT-compatible signature")
	}
}

func TestCollectOpenAIReasoningParts_ObjectWithTextAndIncompatibleSignature(t *testing.T) {
	// Claude signature when target is GPT → incompatible → dropped.
	// Build a valid Claude thinking signature (protobuf-based E format).
	pbPayload := []byte{0x08, 0x0c, 0x12, 0x02, 0x08, 0x02}
	claudeSig := base64.StdEncoding.EncodeToString(pbPayload)
	node := gjson.Parse(`{"text": "reasoning text", "signature": "claude#` + claudeSig + `"}`)
	opts := OpenAIReasoningOptions{
		TargetProvider:     sigcompat.SignatureProviderGPT,
		SignatureBlockKind: sigcompat.SignatureBlockKindGPTReasoning,
	}
	parts := CollectOpenAIReasoningParts(node, opts)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].Text != "reasoning text" {
		t.Fatalf("parts[0].Text = %q, want %q", parts[0].Text, "reasoning text")
	}
	if parts[0].Signature != "" {
		t.Fatalf("expected empty signature for incompatible sig, got %q", parts[0].Signature)
	}
}

func TestCollectOpenAIReasoningParts_ObjectWithEncryptedContent(t *testing.T) {
	gptSig := validGPTReasoningSignature()
	node := gjson.Parse(`{"text": "reasoning text", "encrypted_content": "gpt#` + gptSig + `"}`)
	opts := OpenAIReasoningOptions{
		TargetProvider:     sigcompat.SignatureProviderGPT,
		SignatureBlockKind: sigcompat.SignatureBlockKindGPTReasoning,
	}
	parts := CollectOpenAIReasoningParts(node, opts)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].Signature == "" {
		t.Fatal("expected signature from encrypted_content field")
	}
}

func TestCollectOpenAIReasoningParts_ObjectWithoutTextOrSignature(t *testing.T) {
	// JSON object with none of the recognized keys → empty result.
	node := gjson.Parse(`{"unrelated": "value"}`)
	opts := OpenAIReasoningOptions{}
	parts := CollectOpenAIReasoningParts(node, opts)
	if len(parts) != 0 {
		t.Fatalf("expected 0 parts for unrecognized object, got %d", len(parts))
	}
}

func TestCollectOpenAIReasoningParts_OnlySignatureNoText(t *testing.T) {
	// When there is a compatible signature but no text, a part is still emitted.
	gptSig := validGPTReasoningSignature()
	node := gjson.Parse(`{"signature": "gpt#` + gptSig + `"}`)
	opts := OpenAIReasoningOptions{
		TargetProvider:     sigcompat.SignatureProviderGPT,
		SignatureBlockKind: sigcompat.SignatureBlockKindGPTReasoning,
	}
	parts := CollectOpenAIReasoningParts(node, opts)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part (signature-only), got %d", len(parts))
	}
	if parts[0].Text != "" {
		t.Fatalf("expected empty text, got %q", parts[0].Text)
	}
	if parts[0].Signature == "" {
		t.Fatal("expected non-empty signature")
	}
}

func TestCollectOpenAIReasoningParts_NumberRawFallback(t *testing.T) {
	node := gjson.Parse(`42`)
	opts := OpenAIReasoningOptions{IncludeJSONRawFallback: true}
	parts := CollectOpenAIReasoningParts(node, opts)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part with raw fallback, got %d", len(parts))
	}
	if parts[0].Text != "42" {
		t.Fatalf("parts[0].Text = %q, want %q", parts[0].Text, "42")
	}
}

func TestCollectOpenAIReasoningParts_NumberRawFallbackDisabled(t *testing.T) {
	node := gjson.Parse(`42`)
	opts := OpenAIReasoningOptions{IncludeJSONRawFallback: false}
	parts := CollectOpenAIReasoningParts(node, opts)
	if len(parts) != 0 {
		t.Fatalf("expected 0 parts when raw fallback disabled, got %d", len(parts))
	}
}

func TestCollectOpenAIReasoningParts_NumberRawWithObjectPrefix(t *testing.T) {
	// Number node whose Raw starts with "{" → shouldUseRawFallback returns false.
	node := gjson.Result{Type: gjson.Number, Raw: "{  42}"}
	opts := OpenAIReasoningOptions{IncludeJSONRawFallback: true}
	if got := shouldUseRawFallback(node, opts); got {
		t.Fatal("shouldUseRawFallback should return false for number Raw starting with {")
	}
}

func TestCollectOpenAIReasoningParts_NumberRawWithArrayPrefix(t *testing.T) {
	node := gjson.Result{Type: gjson.Number, Raw: "[  42]"}
	opts := OpenAIReasoningOptions{IncludeJSONRawFallback: true}
	if got := shouldUseRawFallback(node, opts); got {
		t.Fatal("shouldUseRawFallback should return false for number Raw starting with [")
	}
}

func TestCollectOpenAIReasoningParts_DefaultType(t *testing.T) {
	// gjson.Null with no raw fallback → empty.
	t.Run("null_no_fallback", func(t *testing.T) {
		node := gjson.Parse(`null`)
		opts := OpenAIReasoningOptions{IncludeJSONRawFallback: false}
		parts := CollectOpenAIReasoningParts(node, opts)
		if len(parts) != 0 {
			t.Fatalf("expected 0 parts for null, got %d", len(parts))
		}
	})

	// Boolean with IncludeJSONRawFallback=true → still 0 parts, because
	// gjson.True/gjson.False are not gjson.Number, so raw fallback is skipped.
	t.Run("boolean_with_fallback", func(t *testing.T) {
		node := gjson.Parse(`true`)
		opts := OpenAIReasoningOptions{IncludeJSONRawFallback: true}
		parts := CollectOpenAIReasoningParts(node, opts)
		if len(parts) != 0 {
			t.Fatalf("expected 0 parts for boolean with raw fallback, got %d", len(parts))
		}
	})
}

// ---------------------------------------------------------------------------
// CollectOpenAIReasoningPartsFromMessage
// ---------------------------------------------------------------------------

func TestCollectOpenAIReasoningPartsFromMessage_NonArrayContent(t *testing.T) {
	content := gjson.Parse(`"not an array"`)
	reasoning := gjson.Parse(`""`)
	opts := OpenAIReasoningOptions{}
	parts := CollectOpenAIReasoningPartsFromMessage(content, reasoning, opts)
	if len(parts) != 0 {
		t.Fatalf("expected 0 parts, got %d", len(parts))
	}
}

func TestCollectOpenAIReasoningPartsFromMessage_ReasoningFieldOnly(t *testing.T) {
	content := gjson.Parse(`[]`)
	reasoning := gjson.Parse(`"deep thinking"`)
	opts := OpenAIReasoningOptions{}
	parts := CollectOpenAIReasoningPartsFromMessage(content, reasoning, opts)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part from reasoning field, got %d", len(parts))
	}
	if parts[0].Text != "deep thinking" {
		t.Fatalf("parts[0].Text = %q, want %q", parts[0].Text, "deep thinking")
	}
}

func TestCollectOpenAIReasoningPartsFromMessage_ContentReasoningParts(t *testing.T) {
	content := gjson.Parse(`[
		{"type": "reasoning", "text": "step one"},
		{"type": "text", "text": "visible answer"},
		{"type": "reasoning", "text": "step two"}
	]`)
	reasoning := gjson.Parse(`""`)
	opts := OpenAIReasoningOptions{}
	parts := CollectOpenAIReasoningPartsFromMessage(content, reasoning, opts)
	if len(parts) != 2 {
		t.Fatalf("expected 2 reasoning parts from content, got %d", len(parts))
	}
	if parts[0].Text != "step one" {
		t.Fatalf("parts[0].Text = %q, want %q", parts[0].Text, "step one")
	}
	if parts[1].Text != "step two" {
		t.Fatalf("parts[1].Text = %q, want %q", parts[1].Text, "step two")
	}
}

func TestCollectOpenAIReasoningPartsFromMessage_ContentThinkingParts(t *testing.T) {
	content := gjson.Parse(`[
		{"type": "thinking", "thinking": "deep thought"},
		{"type": "text", "text": "answer"}
	]`)
	reasoning := gjson.Parse(`""`)
	opts := OpenAIReasoningOptions{}
	parts := CollectOpenAIReasoningPartsFromMessage(content, reasoning, opts)
	if len(parts) != 1 {
		t.Fatalf("expected 1 thinking part, got %d", len(parts))
	}
	if parts[0].Text != "deep thought" {
		t.Fatalf("parts[0].Text = %q, want %q", parts[0].Text, "deep thought")
	}
}

func TestCollectOpenAIReasoningPartsFromMessage_MergeDedupSameTextAndSig(t *testing.T) {
	gptSig := validGPTReasoningSignature()
	prefixedSig := "gpt#" + gptSig
	content := gjson.Parse(`[{"type": "reasoning", "text": "same text", "signature": "` + prefixedSig + `"}]`)
	reasoning := gjson.Parse(`{"text": "same text", "signature": "` + prefixedSig + `"}`)
	opts := OpenAIReasoningOptions{
		TargetProvider:     sigcompat.SignatureProviderGPT,
		SignatureBlockKind: sigcompat.SignatureBlockKindGPTReasoning,
	}
	parts := CollectOpenAIReasoningPartsFromMessage(content, reasoning, opts)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part after dedup (same text+sig), got %d", len(parts))
	}
}

func TestCollectOpenAIReasoningPartsFromMessage_MergeReplaceEmptySig(t *testing.T) {
	gptSig := validGPTReasoningSignature()
	prefixedSig := "gpt#" + gptSig
	// reasoning field has text without signature, content has same text with signature.
	content := gjson.Parse(`[{"type": "reasoning", "text": "merge me", "signature": "` + prefixedSig + `"}]`)
	reasoning := gjson.Parse(`{"text": "merge me"}`)
	opts := OpenAIReasoningOptions{
		TargetProvider:     sigcompat.SignatureProviderGPT,
		SignatureBlockKind: sigcompat.SignatureBlockKindGPTReasoning,
	}
	parts := CollectOpenAIReasoningPartsFromMessage(content, reasoning, opts)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part after merge, got %d", len(parts))
	}
	if parts[0].Signature == "" {
		t.Fatal("expected signature to be populated from the content part")
	}
}

func TestCollectOpenAIReasoningPartsFromMessage_MergeSkipsKeepExistingSig(t *testing.T) {
	// Use unrelated signatures (not identical key material) to ensure they differ.
	gptSig1 := validGPTReasoningSignature()
	gptSig2 := validGPTReasoningSignatureVariant(64)
	rawSig1 := "gpt#" + gptSig1
	rawSig2 := "gpt#" + gptSig2
	content := gjson.Parse(`[{"type": "reasoning", "text": "same text", "signature": "` + rawSig2 + `"}]`)
	reasoning := gjson.Parse(`{"text": "same text", "signature": "` + rawSig1 + `"}`)
	opts := OpenAIReasoningOptions{
		TargetProvider:     sigcompat.SignatureProviderGPT,
		SignatureBlockKind: sigcompat.SignatureBlockKindGPTReasoning,
	}
	parts := CollectOpenAIReasoningPartsFromMessage(content, reasoning, opts)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts (different signatures not merged), got %d", len(parts))
	}
}

func TestCollectOpenAIReasoningPartsFromMessage_MergeEmptyIncomingSkipped(t *testing.T) {
	content := gjson.Parse(`[{"type": "reasoning", "text": "", "signature": ""}]`)
	reasoning := gjson.Parse(`{"text": "existing"}`)
	opts := OpenAIReasoningOptions{}
	parts := CollectOpenAIReasoningPartsFromMessage(content, reasoning, opts)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part (empty incoming skipped), got %d", len(parts))
	}
	if parts[0].Text != "existing" {
		t.Fatalf("parts[0].Text = %q, want %q", parts[0].Text, "existing")
	}
}

// ---------------------------------------------------------------------------
// CollectOpenAIReasoningTexts
// ---------------------------------------------------------------------------

func TestCollectOpenAIReasoningTexts(t *testing.T) {
	node := gjson.Parse(`["text one", "text two", "text three"]`)
	opts := OpenAIReasoningOptions{}
	texts := CollectOpenAIReasoningTexts(node, opts)
	if len(texts) != 3 {
		t.Fatalf("expected 3 texts, got %d", len(texts))
	}
	if texts[0] != "text one" {
		t.Fatalf("texts[0] = %q, want %q", texts[0], "text one")
	}
	if texts[1] != "text two" {
		t.Fatalf("texts[1] = %q, want %q", texts[1], "text two")
	}
	if texts[2] != "text three" {
		t.Fatalf("texts[2] = %q, want %q", texts[2], "text three")
	}
}

func TestCollectOpenAIReasoningTexts_EmptyTextsFiltered(t *testing.T) {
	// Empty strings in the array should be skipped.
	node := gjson.Parse(`["valid", "", "also valid"]`)
	opts := OpenAIReasoningOptions{}
	texts := CollectOpenAIReasoningTexts(node, opts)
	if len(texts) != 2 {
		t.Fatalf("expected 2 texts (empty filtered), got %d", len(texts))
	}
	if texts[0] != "valid" {
		t.Fatalf("texts[0] = %q, want %q", texts[0], "valid")
	}
	if texts[1] != "also valid" {
		t.Fatalf("texts[1] = %q, want %q", texts[1], "also valid")
	}
}

func TestCollectOpenAIReasoningTexts_NoParts(t *testing.T) {
	node := gjson.Parse(`{}`)
	opts := OpenAIReasoningOptions{}
	texts := CollectOpenAIReasoningTexts(node, opts)
	if len(texts) != 0 {
		t.Fatalf("expected 0 texts, got %d", len(texts))
	}
}

// ---------------------------------------------------------------------------
// JoinOpenAIReasoningTexts
// ---------------------------------------------------------------------------

func TestJoinOpenAIReasoningTexts_MultipleParts(t *testing.T) {
	parts := []OpenAIReasoningPart{
		{Text: "first thought"},
		{Text: "second thought"},
		{Text: "third thought"},
	}
	got := JoinOpenAIReasoningTexts(parts)
	want := "first thought\n\nsecond thought\n\nthird thought"
	if got != want {
		t.Fatalf("JoinOpenAIReasoningTexts = %q, want %q", got, want)
	}
}

func TestJoinOpenAIReasoningTexts_EmptyTextsFiltered(t *testing.T) {
	parts := []OpenAIReasoningPart{
		{Text: "first"},
		{Text: ""},
		{Text: "third"},
	}
	got := JoinOpenAIReasoningTexts(parts)
	want := "first\n\nthird"
	if got != want {
		t.Fatalf("JoinOpenAIReasoningTexts = %q, want %q", got, want)
	}
}

func TestJoinOpenAIReasoningTexts_AllEmpty(t *testing.T) {
	parts := []OpenAIReasoningPart{
		{Text: ""},
		{Text: ""},
	}
	got := JoinOpenAIReasoningTexts(parts)
	if got != "" {
		t.Fatalf("expected empty result, got %q", got)
	}
}

func TestJoinOpenAIReasoningTexts_EmptySlice(t *testing.T) {
	got := JoinOpenAIReasoningTexts(nil)
	if got != "" {
		t.Fatalf("expected empty result for nil, got %q", got)
	}
	got = JoinOpenAIReasoningTexts([]OpenAIReasoningPart{})
	if got != "" {
		t.Fatalf("expected empty result for empty slice, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// FirstStringValue
// ---------------------------------------------------------------------------

func TestFirstStringValue_FirstPathMatches(t *testing.T) {
	node := gjson.Parse(`{"text": "text value", "thinking": "thinking value", "content": "content value"}`)
	got := FirstStringValue(node, "text", "thinking", "content")
	if got != "text value" {
		t.Fatalf("FirstStringValue = %q, want %q", got, "text value")
	}
}

func TestFirstStringValue_SecondPathMatches(t *testing.T) {
	node := gjson.Parse(`{"thinking": "thinking value", "content": "content value"}`)
	got := FirstStringValue(node, "text", "thinking", "content")
	if got != "thinking value" {
		t.Fatalf("FirstStringValue = %q, want %q", got, "thinking value")
	}
}

func TestFirstStringValue_LastPathMatches(t *testing.T) {
	node := gjson.Parse(`{"content": "content value"}`)
	got := FirstStringValue(node, "text", "thinking", "content")
	if got != "content value" {
		t.Fatalf("FirstStringValue = %q, want %q", got, "content value")
	}
}

func TestFirstStringValue_NoMatch(t *testing.T) {
	node := gjson.Parse(`{"other": "value"}`)
	got := FirstStringValue(node, "text", "thinking", "content")
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestFirstStringValue_EmptyStringSkipped(t *testing.T) {
	node := gjson.Parse(`{"text": ""}`)
	got := FirstStringValue(node, "text", "content")
	if got != "" {
		t.Fatalf("expected empty when first path has empty string, got %q", got)
	}
}

func TestFirstStringValue_NonStringTypeSkipped(t *testing.T) {
	node := gjson.Parse(`{"text": 42}`)
	got := FirstStringValue(node, "text", "content")
	if got != "" {
		t.Fatalf("expected empty when type is not string, got %q", got)
	}
}

func TestFirstStringValue_NonExistentPath(t *testing.T) {
	node := gjson.Parse(`{}`)
	got := FirstStringValue(node, "text")
	if got != "" {
		t.Fatalf("expected empty for non-existent path, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// CompatibleSignature
// ---------------------------------------------------------------------------

func TestCompatibleSignature_EmptyTargetProvider(t *testing.T) {
	got := CompatibleSignature("", "some-signature", sigcompat.SignatureBlockKindGPTReasoning)
	if got != "" {
		t.Fatalf("expected empty for empty target provider, got %q", got)
	}
}

func TestCompatibleSignature_CompatibleGPT(t *testing.T) {
	gptSig := validGPTReasoningSignature()
	got := CompatibleSignature(sigcompat.SignatureProviderGPT, gptSig, sigcompat.SignatureBlockKindGPTReasoning)
	if got == "" {
		t.Fatal("expected non-empty signature for compatible GPT")
	}
}

func TestCompatibleSignature_CompatibleGPTWithPrefix(t *testing.T) {
	gptSig := validGPTReasoningSignature()
	got := CompatibleSignature(sigcompat.SignatureProviderGPT, "gpt#"+gptSig, sigcompat.SignatureBlockKindGPTReasoning)
	if got == "" {
		t.Fatal("expected non-empty signature for prefixed GPT signature")
	}
}

func TestCompatibleSignature_IncompatibleClaudeForGPT(t *testing.T) {
	pbPayload := []byte{0x08, 0x0c, 0x12, 0x02, 0x08, 0x02}
	claudeSig := base64.StdEncoding.EncodeToString(pbPayload)
	got := CompatibleSignature(sigcompat.SignatureProviderGPT, "claude#"+claudeSig, sigcompat.SignatureBlockKindClaudeThinking)
	if got != "" {
		t.Fatalf("expected empty for Claude sig targeted at GPT, got %q", got)
	}
}

func TestCompatibleSignature_EmptyBlockKindDefaults(t *testing.T) {
	gptSig := validGPTReasoningSignature()
	got := CompatibleSignature(sigcompat.SignatureProviderGPT, gptSig, "")
	if got == "" {
		t.Fatal("expected non-empty signature with empty block kind defaulting to unknown")
	}
}

func TestCompatibleSignature_GarbageSignature(t *testing.T) {
	got := CompatibleSignature(sigcompat.SignatureProviderGPT, "not-a-valid-signature", sigcompat.SignatureBlockKindGPTReasoning)
	if got != "" {
		t.Fatalf("expected empty for garbage signature, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// shouldUseRawFallback (unexported — tested via CollectOpenAIReasoningParts
// already covers it, but we add direct unit tests here).
// ---------------------------------------------------------------------------

func TestShouldUseRawFallback_Disabled(t *testing.T) {
	node := gjson.Result{Type: gjson.Number, Raw: "42"}
	opts := OpenAIReasoningOptions{IncludeJSONRawFallback: false}
	if shouldUseRawFallback(node, opts) {
		t.Fatal("should be false when IncludeJSONRawFallback is false")
	}
}

func TestShouldUseRawFallback_NonNumber(t *testing.T) {
	node := gjson.Result{Type: gjson.String, Raw: `"hello"`}
	opts := OpenAIReasoningOptions{IncludeJSONRawFallback: true}
	if shouldUseRawFallback(node, opts) {
		t.Fatal("should be false for String type")
	}
}

func TestShouldUseRawFallback_EmptyRaw(t *testing.T) {
	node := gjson.Result{Type: gjson.Number, Raw: ""}
	opts := OpenAIReasoningOptions{IncludeJSONRawFallback: true}
	if shouldUseRawFallback(node, opts) {
		t.Fatal("should be false for empty raw")
	}
}

func TestShouldUseRawFallback_ValidNumber(t *testing.T) {
	node := gjson.Result{Type: gjson.Number, Raw: "42"}
	opts := OpenAIReasoningOptions{IncludeJSONRawFallback: true}
	if !shouldUseRawFallback(node, opts) {
		t.Fatal("should be true for valid number with raw fallback enabled")
	}
}

// ---------------------------------------------------------------------------
// mergeOpenAIReasoningParts (unexported — direct test for edge cases not
// easily reached through CollectOpenAIReasoningPartsFromMessage).
// ---------------------------------------------------------------------------

func TestMergeOpenAIReasoningParts_EmptyIncoming(t *testing.T) {
	parts := []OpenAIReasoningPart{{Text: "existing"}}
	result := mergeOpenAIReasoningParts(parts, OpenAIReasoningPart{Text: "", Signature: ""})
	if len(result) != 1 {
		t.Fatalf("expected 1 part after empty incoming, got %d", len(result))
	}
}

func TestMergeOpenAIReasoningParts_AppendNew(t *testing.T) {
	parts := []OpenAIReasoningPart{{Text: "first"}}
	result := mergeOpenAIReasoningParts(parts, OpenAIReasoningPart{Text: "second", Signature: "sig2"})
	if len(result) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(result))
	}
	if result[1].Text != "second" || result[1].Signature != "sig2" {
		t.Fatalf("result[1] = %+v, want {Text: second, Signature: sig2}", result[1])
	}
}

func TestMergeOpenAIReasoningParts_ExistingSigNotReplacedByEmpty(t *testing.T) {
	parts := []OpenAIReasoningPart{{Text: "same", Signature: "existing"}}
	result := mergeOpenAIReasoningParts(parts, OpenAIReasoningPart{Text: "same", Signature: ""})
	if len(result) != 1 {
		t.Fatalf("expected 1 part (existing sig preserved), got %d", len(result))
	}
	if result[0].Signature != "existing" {
		t.Fatalf("expected existing signature preserved, got %q", result[0].Signature)
	}
}

func TestMergeOpenAIReasoningParts_EmptyReplacedByNewSig(t *testing.T) {
	parts := []OpenAIReasoningPart{{Text: "same", Signature: ""}}
	result := mergeOpenAIReasoningParts(parts, OpenAIReasoningPart{Text: "same", Signature: "new-sig"})
	if len(result) != 1 {
		t.Fatalf("expected 1 part (emptysig replaced), got %d", len(result))
	}
	if result[0].Signature != "new-sig" {
		t.Fatalf("expected new signature, got %q", result[0].Signature)
	}
}

func TestMergeOpenAIReasoningParts_ExactDedup(t *testing.T) {
	parts := []OpenAIReasoningPart{{Text: "same", Signature: "sig"}}
	result := mergeOpenAIReasoningParts(parts, OpenAIReasoningPart{Text: "same", Signature: "sig"})
	if len(result) != 1 {
		t.Fatalf("expected 1 part after exact dedup, got %d", len(result))
	}
}

func TestMergeOpenAIReasoningParts_SameTextDifferentSigAppends(t *testing.T) {
	parts := []OpenAIReasoningPart{{Text: "same", Signature: "sig1"}}
	result := mergeOpenAIReasoningParts(parts, OpenAIReasoningPart{Text: "same", Signature: "sig2"})
	if len(result) != 2 {
		t.Fatalf("expected 2 parts (different sigs, same text), got %d", len(result))
	}
}
