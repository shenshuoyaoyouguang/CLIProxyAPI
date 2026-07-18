package helps

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const codexInputItemIDLimit = 64

// SanitizeCodexInputItemIDs deterministically shortens overlong Responses input
// item IDs and any co-located call_id values that share the same overlong
// string, so tool-call chains remain consistent after truncation.
func SanitizeCodexInputItemIDs(body []byte) []byte {
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return body
	}

	items := input.Array()
	occupied := make(map[string]struct{}, len(items)*2)
	for _, item := range items {
		collectOccupiedCodexID(occupied, item.Get("id"))
		collectOccupiedCodexID(occupied, item.Get("call_id"))
	}

	mapped := make(map[string]string, len(items))
	updated := body
	for index := range items {
		prefix := "input." + strconv.Itoa(index)
		updated = rewriteCodexIDField(updated, items[index].Get("id"), prefix+".id", occupied, mapped)
		updated = rewriteCodexIDField(updated, items[index].Get("call_id"), prefix+".call_id", occupied, mapped)
	}
	return updated
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

func rewriteCodexIDField(body []byte, value gjson.Result, path string, occupied map[string]struct{}, mapped map[string]string) []byte {
	if value.Type != gjson.String {
		return body
	}
	id := value.String()
	if len([]rune(id)) <= codexInputItemIDLimit {
		return body
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

	next, errSet := sjson.SetBytes(body, path, shortened)
	if errSet != nil {
		return body
	}
	return next
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
