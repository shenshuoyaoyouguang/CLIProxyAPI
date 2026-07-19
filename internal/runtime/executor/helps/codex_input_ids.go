package helps

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const codexInputItemIDLimit = 64

// SanitizeCodexInputItemIDs removes encrypted reasoning items whose IDs exceed
// the Codex limit and deterministically shortens other overlong input item IDs
// and any co-located call_id values that share the same overlong string, so
// tool-call chains remain consistent after truncation.
func SanitizeCodexInputItemIDs(body []byte) []byte {
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return body
	}

	items := input.Array()
	occupied := make(map[string]struct{}, len(items)*2)
	for _, item := range items {
		if shouldDropCodexEncryptedReasoningItem(item) {
			continue
		}
		collectOccupiedCodexID(occupied, item.Get("id"))
		collectOccupiedCodexID(occupied, item.Get("call_id"))
	}

	mapped := make(map[string]string, len(items))
	rebuilt := make([]string, 0, len(items))
	changed := false
	for _, item := range items {
		if shouldDropCodexEncryptedReasoningItem(item) {
			changed = true
			continue
		}

		raw := item.Raw
		nextRaw, idChanged := rewriteCodexIDInRaw(raw, "id", occupied, mapped)
		if idChanged {
			raw = nextRaw
			changed = true
		}
		nextRaw, callChanged := rewriteCodexIDInRaw(raw, "call_id", occupied, mapped)
		if callChanged {
			raw = nextRaw
			changed = true
		}
		rebuilt = append(rebuilt, raw)
	}
	if !changed {
		return body
	}

	updated, errSet := sjson.SetRawBytes(body, "input", []byte("["+strings.Join(rebuilt, ",")+"]"))
	if errSet != nil {
		return body
	}
	return updated
}

func shouldDropCodexEncryptedReasoningItem(item gjson.Result) bool {
	if item.Get("type").String() != "reasoning" {
		return false
	}
	itemID := item.Get("id")
	if itemID.Type != gjson.String || len([]rune(itemID.String())) <= codexInputItemIDLimit {
		return false
	}
	encryptedContent := item.Get("encrypted_content")
	return encryptedContent.Type == gjson.String && encryptedContent.String() != ""
}

func collectOccupiedCodexID(occupied map[string]struct{}, value gjson.Result) {
	if value.Type != gjson.String {
		return
	}
	id := value.String()
	if len([]rune(id)) <= codexInputItemIDLimit {
		occupied[id] = struct{}{}
	}
}

func rewriteCodexIDInRaw(raw string, field string, occupied map[string]struct{}, mapped map[string]string) (string, bool) {
	value := gjson.Get(raw, field)
	if value.Type != gjson.String {
		return raw, false
	}
	id := value.String()
	if len([]rune(id)) <= codexInputItemIDLimit {
		return raw, false
	}

	shortened, ok := mapped[id]
	if !ok {
		shortened = shortenCodexInputItemID(id)
		for attempt := 1; ; attempt++ {
			if _, exists := occupied[shortened]; !exists {
				break
			}
			shortened = shortenCodexInputItemIDWithAttempt(id, attempt)
		}
		mapped[id] = shortened
		occupied[shortened] = struct{}{}
	}

	next, errSet := sjson.Set(raw, field, shortened)
	if errSet != nil {
		return raw, false
	}
	return next, true
}

func shortenCodexInputItemID(id string) string {
	return shortenCodexInputItemIDWithAttempt(id, 0)
}

func shortenCodexInputItemIDWithAttempt(id string, attempt int) string {
	runes := []rune(id)
	if len(runes) <= codexInputItemIDLimit {
		return id
	}

	hashInput := id
	if attempt > 0 {
		hashInput += "\x00" + strconv.Itoa(attempt)
	}
	sum := sha256.Sum256([]byte(hashInput))
	suffix := "_" + hex.EncodeToString(sum[:8])
	prefixLength := codexInputItemIDLimit - len(suffix)
	return string(runes[:prefixLength]) + suffix
}
