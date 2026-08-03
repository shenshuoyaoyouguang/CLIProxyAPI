package helps

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// zeroWidthSpace is the Unicode zero-width space character used for obfuscation.
const zeroWidthSpace = "\u200B"

// isASCIIWordRune reports whether r can sit on a \b boundary (ASCII \w).
func isASCIIWordRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_'
}

// SensitiveWordMatcher holds the compiled regex for matching sensitive words.
type SensitiveWordMatcher struct {
	regex *regexp.Regexp
}

// BuildSensitiveWordMatcher compiles a regex from the word list.
// Words are sorted by length (longest first) for proper matching.
func BuildSensitiveWordMatcher(words []string) *SensitiveWordMatcher {
	if len(words) == 0 {
		return nil
	}

	// Filter and normalize words
	var validWords []string
	for _, w := range words {
		w = strings.TrimSpace(w)
		if utf8.RuneCountInString(w) >= 2 && !strings.Contains(w, zeroWidthSpace) {
			validWords = append(validWords, w)
		}
	}

	if len(validWords) == 0 {
		return nil
	}

	// Sort by length (longest first) for proper matching
	sort.Slice(validWords, func(i, j int) bool {
		return len(validWords[i]) > len(validWords[j])
	})

	// Escape and join
	escaped := make([]string, len(validWords))
	for i, w := range validWords {
		escaped[i] = regexp.QuoteMeta(w)
	}

	// Wrap word-character edges in \b so a short word like "pass" does not
	// corrupt a longer word like "password". Words that start or end with a
	// non-word character (spaces, punctuation) keep no boundary on that side,
	// and non-ASCII words (e.g. Chinese) keep substring matching.
	patternParts := make([]string, len(escaped))
	for i, w := range validWords {
		part := escaped[i]
		first, _ := utf8.DecodeRuneInString(w)
		if isASCIIWordRune(first) {
			part = `\b` + part
		}
		last, _ := utf8.DecodeLastRuneInString(w)
		if isASCIIWordRune(last) {
			part += `\b`
		}
		patternParts[i] = part
	}

	pattern := "(?i)" + strings.Join(patternParts, "|")
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}

	return &SensitiveWordMatcher{regex: re}
}

// obfuscateWord inserts a zero-width space after the first grapheme.
func obfuscateWord(word string) string {
	if strings.Contains(word, zeroWidthSpace) {
		return word
	}

	// Get first rune
	r, size := utf8.DecodeRuneInString(word)
	if r == utf8.RuneError || size >= len(word) {
		return word
	}

	return string(r) + zeroWidthSpace + word[size:]
}

// obfuscateText replaces all sensitive words in the text.
func (m *SensitiveWordMatcher) obfuscateText(text string) string {
	if m == nil || m.regex == nil {
		return text
	}
	return m.regex.ReplaceAllStringFunc(text, obfuscateWord)
}

// ObfuscateSensitiveWords processes the payload and obfuscates sensitive words
// in system blocks and message content.
func ObfuscateSensitiveWords(payload []byte, matcher *SensitiveWordMatcher) []byte {
	if matcher == nil || matcher.regex == nil {
		return payload
	}

	// Obfuscate in system blocks
	payload = obfuscateSystemBlocks(payload, matcher)

	// Obfuscate in messages
	payload = obfuscateMessages(payload, matcher)

	return payload
}

// obfuscateSystemBlocks obfuscates sensitive words in system blocks.
func obfuscateSystemBlocks(payload []byte, matcher *SensitiveWordMatcher) []byte {
	system := gjson.GetBytes(payload, "system")
	if !system.Exists() {
		return payload
	}

	if system.IsArray() {
		modified := false
		system.ForEach(func(key, value gjson.Result) bool {
			if value.Get("type").String() == "text" {
				text := value.Get("text").String()
				obfuscated := matcher.obfuscateText(text)
				if obfuscated != text {
					path := "system." + key.String() + ".text"
					payload, _ = sjson.SetBytes(payload, path, obfuscated)
					modified = true
				}
			}
			return true
		})
		if modified {
			return payload
		}
	} else if system.Type == gjson.String {
		text := system.String()
		obfuscated := matcher.obfuscateText(text)
		if obfuscated != text {
			payload, _ = sjson.SetBytes(payload, "system", obfuscated)
		}
	}

	return payload
}

// obfuscateMessages obfuscates sensitive words in message content.
func obfuscateMessages(payload []byte, matcher *SensitiveWordMatcher) []byte {
	messages := gjson.GetBytes(payload, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return payload
	}

	messages.ForEach(func(msgKey, msg gjson.Result) bool {
		content := msg.Get("content")
		if !content.Exists() {
			return true
		}

		msgPath := "messages." + msgKey.String()
		payload = obfuscateMessageContent(payload, matcher, msgPath+".content", content)

		// Scrub tool_use.input argument values too: keys stay untouched so the
		// upstream tool contract is preserved.
		if content.IsArray() {
			content.ForEach(func(blockKey, block gjson.Result) bool {
				if block.Get("type").String() == "tool_use" {
					if input := block.Get("input"); input.Exists() && (input.IsObject() || input.IsArray()) {
						if scrubbed, ok := scrubJSONStringValues(input.Raw, matcher); ok {
							payload, _ = sjson.SetRawBytes(payload, msgPath+".content."+blockKey.String()+".input", []byte(scrubbed))
						}
					}
				}
				return true
			})
		}

		return true
	})

	return payload
}

// obfuscateMessageContent recursively obfuscates text inside a content value
// (string or array of blocks), descending into tool_result content arrays.
func obfuscateMessageContent(payload []byte, matcher *SensitiveWordMatcher, path string, content gjson.Result) []byte {
	if content.Type == gjson.String {
		text := content.String()
		obfuscated := matcher.obfuscateText(text)
		if obfuscated != text {
			payload, _ = sjson.SetBytes(payload, path, obfuscated)
		}
		return payload
	}
	if !content.IsArray() {
		return payload
	}
	content.ForEach(func(blockKey, block gjson.Result) bool {
		blockPath := path + "." + blockKey.String()
		switch block.Get("type").String() {
		case "text":
			text := block.Get("text").String()
			obfuscated := matcher.obfuscateText(text)
			if obfuscated != text {
				payload, _ = sjson.SetBytes(payload, blockPath+".text", obfuscated)
			}
		case "tool_result":
			if nested := block.Get("content"); nested.Exists() {
				payload = obfuscateMessageContent(payload, matcher, blockPath+".content", nested)
			}
		}
		return true
	})
	return payload
}

// scrubJSONStringValues re-encodes rawJSON with sensitive words removed from
// every string value; object keys are left untouched. Returns ok=false when
// nothing changed or the input is not decodable JSON.
func scrubJSONStringValues(rawJSON string, matcher *SensitiveWordMatcher) (string, bool) {
	var value any
	if err := json.Unmarshal([]byte(rawJSON), &value); err != nil {
		return "", false
	}
	changed := false
	var walk func(any) any
	walk = func(v any) any {
		switch t := v.(type) {
		case string:
			scrubbed := matcher.obfuscateText(t)
			if scrubbed != t {
				changed = true
				return scrubbed
			}
			return t
		case map[string]any:
			for k, item := range t {
				t[k] = walk(item)
			}
			return t
		case []any:
			for i, item := range t {
				t[i] = walk(item)
			}
			return t
		default:
			return v
		}
	}
	value = walk(value)
	if !changed {
		return "", false
	}
	reencoded, err := json.Marshal(value)
	if err != nil {
		return "", false
	}
	return string(reencoded), true
}
