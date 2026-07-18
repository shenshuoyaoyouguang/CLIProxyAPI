package helps

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestSanitizeCodexInputItemIDsBoundaries(t *testing.T) {
	id64 := strings.Repeat("a", 64)
	id65 := strings.Repeat("b", 65)
	unicode65 := strings.Repeat("界", 65)
	body := []byte(`{"input":[{"id":"` + id64 + `"},{"id":"` + id65 + `"},{"id":"` + unicode65 + `"}]}`)

	got := SanitizeCodexInputItemIDs(body)

	if actual := gjson.GetBytes(got, "input.0.id").String(); actual != id64 {
		t.Fatalf("64-character ID changed: %q", actual)
	}
	for _, path := range []string{"input.1.id", "input.2.id"} {
		actual := gjson.GetBytes(got, path).String()
		if len([]rune(actual)) != 64 {
			t.Fatalf("%s length = %d, want 64: %q", path, len([]rune(actual)), actual)
		}
	}
}

func TestSanitizeCodexInputItemIDsAvoidsExistingIDCollision(t *testing.T) {
	longID := strings.Repeat("grok-item-", 10)
	collidingValidID := shortenCodexInputItemID(longID)
	body := []byte(`{"input":[{"id":"` + longID + `"},{"id":"` + collidingValidID + `"}]}`)

	first := SanitizeCodexInputItemIDs(body)
	second := SanitizeCodexInputItemIDs(body)

	shortened := gjson.GetBytes(first, "input.0.id").String()
	if shortened == collidingValidID {
		t.Fatalf("shortened ID collided with an existing valid ID: %q", shortened)
	}
	if len([]rune(shortened)) > 64 {
		t.Fatalf("shortened ID length = %d, want at most 64", len([]rune(shortened)))
	}
	if actual := gjson.GetBytes(first, "input.1.id").String(); actual != collidingValidID {
		t.Fatalf("existing valid ID changed: %q", actual)
	}
	if actual := gjson.GetBytes(second, "input.0.id").String(); actual != shortened {
		t.Fatalf("collision resolution is not deterministic: first=%q second=%q", shortened, actual)
	}
}

func TestSanitizeCodexInputItemIDsLeavesUnsupportedPayloadsUnchanged(t *testing.T) {
	for _, body := range [][]byte{
		[]byte(`not-json`),
		[]byte(`{"input":{"id":"item-1"}}`),
		[]byte(`{"input":[1,{"id":2},{"id":"item-1"}]}`),
	} {
		if got := string(SanitizeCodexInputItemIDs(body)); got != string(body) {
			t.Fatalf("payload changed: got=%q want=%q", got, body)
		}
	}
}

func TestSanitizeCodexInputItemIDsRewritesMatchingCallIDs(t *testing.T) {
	longID := strings.Repeat("call-item-", 10)
	body := []byte(`{"input":[` +
		`{"type":"function_call","id":"` + longID + `","call_id":"` + longID + `","name":"lookup","arguments":"{}"},` +
		`{"type":"function_call_output","id":"` + longID + `","call_id":"` + longID + `","output":"ok"},` +
		`{"type":"function_call","id":"short-id","call_id":"call-1","name":"other","arguments":"{}"}` +
		`]}`)

	got := SanitizeCodexInputItemIDs(body)
	shortened := gjson.GetBytes(got, "input.0.id").String()
	if shortened == longID || len([]rune(shortened)) > 64 {
		t.Fatalf("id not shortened: %q", shortened)
	}
	if actual := gjson.GetBytes(got, "input.0.call_id").String(); actual != shortened {
		t.Fatalf("function call_id = %q, want same shortened id %q", actual, shortened)
	}
	if actual := gjson.GetBytes(got, "input.1.id").String(); actual != shortened {
		t.Fatalf("output id = %q, want %q", actual, shortened)
	}
	if actual := gjson.GetBytes(got, "input.1.call_id").String(); actual != shortened {
		t.Fatalf("output call_id = %q, want %q", actual, shortened)
	}
	if actual := gjson.GetBytes(got, "input.2.call_id").String(); actual != "call-1" {
		t.Fatalf("unrelated short call_id changed: %q", actual)
	}
}
