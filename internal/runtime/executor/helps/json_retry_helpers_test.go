package helps

import (
	"testing"
	"time"
)

func TestParseRetryDelay(t *testing.T) {
	t.Run("Google RetryInfo with retryDelay", func(t *testing.T) {
		body := []byte(`{
			"error": {
				"code": 429,
				"message": "quota exceeded",
				"details": [
					{
						"@type": "type.googleapis.com/google.rpc.RetryInfo",
						"retryDelay": "0.479417207s"
					}
				]
			}
		}`)
		got, err := ParseRetryDelay(body)
		if err != nil {
			t.Fatalf("ParseRetryDelay() error = %v", err)
		}
		if got == nil {
			t.Fatal("ParseRetryDelay() returned nil")
		}
		want := 479417207 * time.Nanosecond
		if *got != want {
			t.Fatalf("ParseRetryDelay() = %v, want %v", *got, want)
		}
	})

	t.Run("Google RetryInfo with seconds", func(t *testing.T) {
		body := []byte(`{
			"error": {
				"code": 429,
				"message": "rate limit",
				"details": [
					{
						"@type": "type.googleapis.com/google.rpc.RetryInfo",
						"retryDelay": "5s"
					}
				]
			}
		}`)
		got, err := ParseRetryDelay(body)
		if err != nil {
			t.Fatalf("ParseRetryDelay() error = %v", err)
		}
		if got == nil {
			t.Fatal("ParseRetryDelay() returned nil")
		}
		want := 5 * time.Second
		if *got != want {
			t.Fatalf("ParseRetryDelay() = %v, want %v", *got, want)
		}
	})

	t.Run("Google ErrorInfo with quotaResetDelay", func(t *testing.T) {
		body := []byte(`{
			"error": {
				"code": 429,
				"message": "quota exceeded",
				"details": [
					{
						"@type": "type.googleapis.com/google.rpc.ErrorInfo",
						"metadata": {
							"quotaResetDelay": "30s"
						}
					}
				]
			}
		}`)
		got, err := ParseRetryDelay(body)
		if err != nil {
			t.Fatalf("ParseRetryDelay() error = %v", err)
		}
		if got == nil {
			t.Fatal("ParseRetryDelay() returned nil")
		}
		want := 30 * time.Second
		if *got != want {
			t.Fatalf("ParseRetryDelay() = %v, want %v", *got, want)
		}
	})

	t.Run("Google ErrorInfo with human-readable quotaResetDelay", func(t *testing.T) {
		body := []byte(`{
			"error": {
				"code": 429,
				"message": "quota exceeded",
				"details": [
					{
						"@type": "type.googleapis.com/google.rpc.ErrorInfo",
						"metadata": {
							"quotaResetDelay": "5m"
						}
					}
				]
			}
		}`)
		got, err := ParseRetryDelay(body)
		if err != nil {
			t.Fatalf("ParseRetryDelay() error = %v", err)
		}
		if got == nil {
			t.Fatal("ParseRetryDelay() returned nil")
		}
		want := 5 * time.Minute
		if *got != want {
			t.Fatalf("ParseRetryDelay() = %v, want %v", *got, want)
		}
	})

	t.Run("error message with seconds pattern", func(t *testing.T) {
		body := []byte(`{
			"error": {
				"code": 429,
				"message": "Rate limit exceeded. Try again after 120s."
			}
		}`)
		got, err := ParseRetryDelay(body)
		if err != nil {
			t.Fatalf("ParseRetryDelay() error = %v", err)
		}
		if got == nil {
			t.Fatal("ParseRetryDelay() returned nil")
		}
		want := 120 * time.Second
		if *got != want {
			t.Fatalf("ParseRetryDelay() = %v, want %v", *got, want)
		}
	})

	t.Run("error message with human-readable duration", func(t *testing.T) {
		body := []byte(`{
			"error": {
				"code": 429,
				"message": "Rate limit exceeded. Try again after 2m30s."
			}
		}`)
		got, err := ParseRetryDelay(body)
		if err != nil {
			t.Fatalf("ParseRetryDelay() error = %v", err)
		}
		if got == nil {
			t.Fatal("ParseRetryDelay() returned nil")
		}
		want := 2*time.Minute + 30*time.Second
		if *got != want {
			t.Fatalf("ParseRetryDelay() = %v, want %v", *got, want)
		}
	})

	t.Run("error message with hours", func(t *testing.T) {
		body := []byte(`{
			"error": {
				"code": 429,
				"message": "Quota exceeded. Try again after 1h30m."
			}
		}`)
		got, err := ParseRetryDelay(body)
		if err != nil {
			t.Fatalf("ParseRetryDelay() error = %v", err)
		}
		if got == nil {
			t.Fatal("ParseRetryDelay() returned nil")
		}
		want := 1*time.Hour + 30*time.Minute
		if *got != want {
			t.Fatalf("ParseRetryDelay() = %v, want %v", *got, want)
		}
	})

	t.Run("no retry info returns error", func(t *testing.T) {
		body := []byte(`{
			"error": {
				"code": 400,
				"message": "Bad request"
			}
		}`)
		got, err := ParseRetryDelay(body)
		if err == nil {
			t.Fatalf("ParseRetryDelay() error = nil, want error")
		}
		if got != nil {
			t.Fatalf("ParseRetryDelay() = %v, want nil", *got)
		}
	})

	t.Run("empty body returns error", func(t *testing.T) {
		got, err := ParseRetryDelay(nil)
		if err == nil {
			t.Fatalf("ParseRetryDelay() error = nil, want error")
		}
		if got != nil {
			t.Fatalf("ParseRetryDelay() = %v, want nil", *got)
		}
	})

	t.Run("RetryInfo takes priority over ErrorInfo", func(t *testing.T) {
		body := []byte(`{
			"error": {
				"code": 429,
				"message": "quota exceeded",
				"details": [
					{
						"@type": "type.googleapis.com/google.rpc.RetryInfo",
						"retryDelay": "10s"
					},
					{
						"@type": "type.googleapis.com/google.rpc.ErrorInfo",
						"metadata": {
							"quotaResetDelay": "60s"
						}
					}
				]
			}
		}`)
		got, err := ParseRetryDelay(body)
		if err != nil {
			t.Fatalf("ParseRetryDelay() error = %v", err)
		}
		if got == nil {
			t.Fatal("ParseRetryDelay() returned nil")
		}
		want := 10 * time.Second
		if *got != want {
			t.Fatalf("ParseRetryDelay() = %v, want %v (RetryInfo should take priority)", *got, want)
		}
	})

	t.Run("RetryInfo with empty retryDelay falls through to ErrorInfo", func(t *testing.T) {
		body := []byte(`{
			"error": {
				"code": 429,
				"message": "quota exceeded",
				"details": [
					{
						"@type": "type.googleapis.com/google.rpc.RetryInfo",
						"retryDelay": ""
					},
					{
						"@type": "type.googleapis.com/google.rpc.ErrorInfo",
						"metadata": {
							"quotaResetDelay": "25s"
						}
					}
				]
			}
		}`)
		got, err := ParseRetryDelay(body)
		if err != nil {
			t.Fatalf("ParseRetryDelay() error = %v", err)
		}
		if got == nil {
			t.Fatal("ParseRetryDelay() returned nil")
		}
		want := 25 * time.Second
		if *got != want {
			t.Fatalf("ParseRetryDelay() = %v, want %v", *got, want)
		}
	})

	t.Run("RetryInfo with invalid duration returns error immediately", func(t *testing.T) {
		// Note: The current implementation returns an error immediately when
		// RetryInfo has an invalid duration, rather than falling through to ErrorInfo.
		body := []byte(`{
			"error": {
				"code": 429,
				"message": "quota exceeded",
				"details": [
					{
						"@type": "type.googleapis.com/google.rpc.RetryInfo",
						"retryDelay": "invalid"
					},
					{
						"@type": "type.googleapis.com/google.rpc.ErrorInfo",
						"metadata": {
							"quotaResetDelay": "15s"
						}
					}
				]
			}
		}`)
		got, err := ParseRetryDelay(body)
		if err == nil {
			t.Fatalf("ParseRetryDelay() error = nil, want error for invalid RetryInfo duration")
		}
		if got != nil {
			t.Fatalf("ParseRetryDelay() = %v, want nil", *got)
		}
	})

	t.Run("invalid message pattern returns error", func(t *testing.T) {
		body := []byte(`{
			"error": {
				"code": 429,
				"message": "Rate limit exceeded. No retry information."
			}
		}`)
		got, err := ParseRetryDelay(body)
		if err == nil {
			t.Fatalf("ParseRetryDelay() error = nil, want error")
		}
		if got != nil {
			t.Fatalf("ParseRetryDelay() = %v, want nil", *got)
		}
	})

	t.Run("message with zero seconds returns zero duration", func(t *testing.T) {
		// Note: The seconds pattern (after\s+(\d+)s\.?) doesn't check for zero,
		// so it returns 0s. The human-readable pattern checks duration > 0,
		// but the seconds pattern matches first.
		body := []byte(`{
			"error": {
				"code": 429,
				"message": "Rate limit exceeded. Try again after 0s."
			}
		}`)
		got, err := ParseRetryDelay(body)
		if err != nil {
			t.Fatalf("ParseRetryDelay() error = %v", err)
		}
		if got == nil {
			t.Fatal("ParseRetryDelay() returned nil")
		}
		want := 0 * time.Second
		if *got != want {
			t.Fatalf("ParseRetryDelay() = %v, want %v", *got, want)
		}
	})
}

func TestDeleteJSONField(t *testing.T) {
	t.Run("delete top-level field", func(t *testing.T) {
		body := []byte(`{"a":1,"b":2}`)
		got := DeleteJSONField(body, "a")
		want := []byte(`{"b":2}`)
		if string(got) != string(want) {
			t.Fatalf("DeleteJSONField(%s, 'a') = %s, want %s", body, got, want)
		}
	})

	t.Run("delete nested field", func(t *testing.T) {
		body := []byte(`{"a":{"b":1,"c":2}}`)
		got := DeleteJSONField(body, "a.b")
		want := []byte(`{"a":{"c":2}}`)
		if string(got) != string(want) {
			t.Fatalf("DeleteJSONField(%s, 'a.b') = %s, want %s", body, got, want)
		}
	})

	t.Run("delete non-existent field returns body unchanged", func(t *testing.T) {
		body := []byte(`{"a":1}`)
		got := DeleteJSONField(body, "nonexistent")
		if string(got) != string(body) {
			t.Fatalf("DeleteJSONField(%s, 'nonexistent') = %s, want %s", body, got, body)
		}
	})

	t.Run("empty key returns body unchanged", func(t *testing.T) {
		body := []byte(`{"a":1}`)
		got := DeleteJSONField(body, "")
		if string(got) != string(body) {
			t.Fatalf("DeleteJSONField(%s, '') = %s, want %s", body, got, body)
		}
	})

	t.Run("empty body returns body unchanged", func(t *testing.T) {
		got := DeleteJSONField(nil, "a")
		if got != nil {
			t.Fatalf("DeleteJSONField(nil, 'a') = %s, want nil", got)
		}
	})

	t.Run("delete last field in object", func(t *testing.T) {
		body := []byte(`{"a":1}`)
		got := DeleteJSONField(body, "a")
		want := []byte(`{}`)
		if string(got) != string(want) {
			t.Fatalf("DeleteJSONField(%s, 'a') = %s, want %s", body, got, want)
		}
	})

	t.Run("delete array element", func(t *testing.T) {
		body := []byte(`{"items":[{"id":1},{"id":2}]}`)
		got := DeleteJSONField(body, "items.0")
		want := []byte(`{"items":[{"id":2}]}`)
		if string(got) != string(want) {
			t.Fatalf("DeleteJSONField(%s, 'items.0') = %s, want %s", body, got, want)
		}
	})
}
