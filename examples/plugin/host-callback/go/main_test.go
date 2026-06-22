package main

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// decodeEnvHC is a test helper that unmarshals an envelope for the host-callback plugin.
func decodeEnvHC(t *testing.T, raw []byte) (bool, json.RawMessage, *envelopeError) {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	return env.OK, env.Result, env.Error
}

// managementRegisterResult holds the parsed response for management.register.
type managementRegisterResult struct {
	// The new API uses "resources" not "routes".
	Resources []struct {
		Path        string `json:"Path"`
		Menu        string `json:"Menu"`
		Description string `json:"Description"`
	} `json:"resources"`
	// "routes" should no longer be present.
	Routes []struct {
		Method string `json:"Method"`
		Path   string `json:"Path"`
	} `json:"routes"`
}

// managementHandleResult holds the parsed response for management.handle.
type managementHandleResult struct {
	StatusCode int                 `json:"StatusCode"`
	Headers    map[string][]string `json:"Headers"`
	Body       []byte              `json:"Body"`
}

// TestHandleMethod_Register verifies plugin.register returns a successful envelope.
func TestHandleMethod_Register(t *testing.T) {
	for _, method := range []string{"plugin.register", "plugin.reconfigure"} {
		t.Run(method, func(t *testing.T) {
			raw, err := handleMethod(method)
			if err != nil {
				t.Fatalf("handleMethod(%q) error = %v", method, err)
			}
			ok, _, _ := decodeEnvHC(t, raw)
			if !ok {
				t.Fatalf("handleMethod(%q) ok = false, want true", method)
			}
		})
	}
}

// TestHandleMethod_ManagementRegister_UsesResourcesKey verifies that management.register
// now returns a "resources" array (not "routes") as changed in this PR.
func TestHandleMethod_ManagementRegister_UsesResourcesKey(t *testing.T) {
	raw, err := handleMethod("management.register")
	if err != nil {
		t.Fatalf("handleMethod(management.register) error = %v", err)
	}
	ok, result, _ := decodeEnvHC(t, raw)
	if !ok {
		t.Fatalf("handleMethod(management.register) ok = false, want true")
	}

	var reg managementRegisterResult
	if err := json.Unmarshal(result, &reg); err != nil {
		t.Fatalf("unmarshal management register result: %v", err)
	}

	// The PR changed "routes" to "resources" — ensure resources is populated.
	if len(reg.Resources) == 0 {
		t.Errorf("resources array is empty, want at least one resource entry")
	}
	// The old "routes" key must not be present / must be empty.
	if len(reg.Routes) != 0 {
		t.Errorf("routes array is still present with %d entries, want empty (PR changed to resources)", len(reg.Routes))
	}
}

// TestHandleMethod_ManagementRegister_PathFormat verifies the resource path is relative ("/status").
func TestHandleMethod_ManagementRegister_PathFormat(t *testing.T) {
	raw, err := handleMethod("management.register")
	if err != nil {
		t.Fatalf("handleMethod(management.register) error = %v", err)
	}
	_, result, _ := decodeEnvHC(t, raw)

	var reg managementRegisterResult
	if err := json.Unmarshal(result, &reg); err != nil {
		t.Fatalf("unmarshal management register result: %v", err)
	}
	if len(reg.Resources) == 0 {
		t.Fatal("resources array is empty")
	}
	// The new path format is relative — must not contain the full plugin path prefix.
	path := reg.Resources[0].Path
	if !strings.HasPrefix(path, "/") {
		t.Errorf("resource Path = %q, want to start with '/'", path)
	}
	// Must not embed the full "/plugins/<id>/..." prefix style (old routes format).
	if strings.Contains(path, "/plugins/") {
		t.Errorf("resource Path = %q still embeds /plugins/ prefix, PR changed to relative path", path)
	}
}

// TestHandleMethod_ManagementRegister_MenuAndDescription verifies menu and description are set.
func TestHandleMethod_ManagementRegister_MenuAndDescription(t *testing.T) {
	raw, err := handleMethod("management.register")
	if err != nil {
		t.Fatalf("handleMethod(management.register) error = %v", err)
	}
	_, result, _ := decodeEnvHC(t, raw)

	var reg managementRegisterResult
	if err := json.Unmarshal(result, &reg); err != nil {
		t.Fatalf("unmarshal management register result: %v", err)
	}
	if len(reg.Resources) == 0 {
		t.Fatal("resources array is empty")
	}
	if reg.Resources[0].Menu == "" {
		t.Errorf("resource Menu is empty, want non-empty")
	}
	if reg.Resources[0].Description == "" {
		t.Errorf("resource Description is empty, want non-empty")
	}
}

// TestHandleMethod_ManagementHandle_ContentType verifies management.handle now returns
// text/html content-type (changed from application/json in this PR).
func TestHandleMethod_ManagementHandle_ContentType(t *testing.T) {
	raw, err := handleMethod("management.handle")
	if err != nil {
		t.Fatalf("handleMethod(management.handle) error = %v", err)
	}
	ok, result, _ := decodeEnvHC(t, raw)
	if !ok {
		t.Fatalf("handleMethod(management.handle) ok = false, want true")
	}

	var resp managementHandleResult
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal management handle result: %v", err)
	}
	contentType := resp.Headers["content-type"]
	if len(contentType) == 0 {
		t.Fatalf("content-type header is missing")
	}
	if !strings.Contains(contentType[0], "text/html") {
		t.Errorf("content-type = %q, want to contain %q (PR changed from application/json)", contentType[0], "text/html")
	}
	// Explicitly verify the old application/json is NOT returned.
	if strings.Contains(contentType[0], "application/json") {
		t.Errorf("content-type still contains application/json, want text/html (PR changed this)")
	}
}

// TestHandleMethod_ManagementHandle_StatusCode verifies management.handle returns 200.
func TestHandleMethod_ManagementHandle_StatusCode(t *testing.T) {
	raw, err := handleMethod("management.handle")
	if err != nil {
		t.Fatalf("handleMethod(management.handle) error = %v", err)
	}
	_, result, _ := decodeEnvHC(t, raw)

	var resp managementHandleResult
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal management handle result: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
}

// TestHandleMethod_ManagementHandle_BodyIsHTML verifies the body decodes to HTML content.
func TestHandleMethod_ManagementHandle_BodyIsHTML(t *testing.T) {
	raw, err := handleMethod("management.handle")
	if err != nil {
		t.Fatalf("handleMethod(management.handle) error = %v", err)
	}
	_, result, _ := decodeEnvHC(t, raw)

	var resp managementHandleResult
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal management handle result: %v", err)
	}
	if len(resp.Body) == 0 {
		t.Fatal("response body is empty")
	}
	// Body is base64-encoded in JSON; the test struct decodes it.
	bodyStr := string(resp.Body)
	if !strings.Contains(strings.ToLower(bodyStr), "<!doctype html>") &&
		!strings.Contains(strings.ToLower(bodyStr), "<html>") &&
		!strings.Contains(strings.ToLower(bodyStr), "<title>") {
		// Try decoding as base64 (in case the struct didn't decode it).
		decoded, errDecode := base64.StdEncoding.DecodeString(bodyStr)
		if errDecode == nil {
			bodyStr = string(decoded)
		}
		if !strings.Contains(strings.ToLower(bodyStr), "<!doctype html>") &&
			!strings.Contains(strings.ToLower(bodyStr), "<title>") {
			limit := 100
			if len(bodyStr) < limit {
				limit = len(bodyStr)
			}
			t.Errorf("response body does not appear to be HTML: %q", bodyStr[:limit])
		}
	}
}

// TestHandleMethod_UnknownMethod verifies unknown methods return an error envelope.
func TestHandleMethod_UnknownMethod(t *testing.T) {
	raw, err := handleMethod("no.such.method")
	if err != nil {
		t.Fatalf("handleMethod(unknown) returned error = %v, want nil", err)
	}
	ok, _, errInfo := decodeEnvHC(t, raw)
	if ok {
		t.Fatalf("handleMethod(unknown) ok = true, want false")
	}
	if errInfo == nil || errInfo.Code != "unknown_method" {
		t.Errorf("error code = %v, want %q", errInfo, "unknown_method")
	}
}

// TestOkEnvelopeJSON verifies okEnvelopeJSON wraps a raw JSON string in a success envelope.
func TestOkEnvelopeJSON(t *testing.T) {
	raw, err := okEnvelopeJSON(`{"key":"value"}`)
	if err != nil {
		t.Fatalf("okEnvelopeJSON error = %v", err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.OK {
		t.Errorf("OK = false, want true")
	}
	if env.Error != nil {
		t.Errorf("Error is set, want nil")
	}
	// The result should contain the raw JSON.
	if !strings.Contains(string(env.Result), "value") {
		t.Errorf("result does not contain original JSON value: %s", env.Result)
	}
}

// TestErrorEnvelope verifies errorEnvelope produces a failed envelope.
func TestErrorEnvelope(t *testing.T) {
	raw := errorEnvelope("test_code", "test msg")
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.OK {
		t.Errorf("OK = true, want false")
	}
	if env.Error == nil || env.Error.Code != "test_code" {
		t.Errorf("error code = %v, want %q", env.Error, "test_code")
	}
}
