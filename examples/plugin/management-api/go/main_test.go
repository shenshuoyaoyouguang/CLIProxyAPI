package main

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// decodeEnvMgmt is a test helper that unmarshals an envelope for the management-api plugin.
func decodeEnvMgmt(t *testing.T, raw []byte) (bool, json.RawMessage, *envelopeError) {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	return env.OK, env.Result, env.Error
}

// managementRegisterResultMgmt parses the management.register response.
type managementRegisterResultMgmt struct {
	// PR changed "routes" to "resources".
	Resources []struct {
		Path        string `json:"Path"`
		Menu        string `json:"Menu"`
		Description string `json:"Description"`
	} `json:"resources"`
	Routes []struct {
		Method string `json:"Method"`
		Path   string `json:"Path"`
	} `json:"routes"`
}

// managementHandleResultMgmt parses the management.handle response.
type managementHandleResultMgmt struct {
	StatusCode int               `json:"StatusCode"`
	Headers    map[string][]string `json:"Headers"`
	Body       []byte            `json:"Body"`
}

// TestHandleMethod_Register verifies plugin.register and plugin.reconfigure return success.
func TestHandleMethod_Register(t *testing.T) {
	for _, method := range []string{"plugin.register", "plugin.reconfigure"} {
		t.Run(method, func(t *testing.T) {
			raw, err := handleMethod(method)
			if err != nil {
				t.Fatalf("handleMethod(%q) error = %v", method, err)
			}
			ok, _, _ := decodeEnvMgmt(t, raw)
			if !ok {
				t.Fatalf("handleMethod(%q) ok = false, want true", method)
			}
		})
	}
}

// TestHandleMethod_ManagementRegister_UsesResourcesKey verifies management.register
// uses the new "resources" key instead of the old "routes" key.
func TestHandleMethod_ManagementRegister_UsesResourcesKey(t *testing.T) {
	raw, err := handleMethod("management.register")
	if err != nil {
		t.Fatalf("handleMethod(management.register) error = %v", err)
	}
	ok, result, _ := decodeEnvMgmt(t, raw)
	if !ok {
		t.Fatalf("handleMethod(management.register) ok = false, want true")
	}

	var reg managementRegisterResultMgmt
	if err := json.Unmarshal(result, &reg); err != nil {
		t.Fatalf("unmarshal management register result: %v", err)
	}

	// PR changed "routes" to "resources" — ensure resources is populated.
	if len(reg.Resources) == 0 {
		t.Errorf("resources array is empty, want at least one entry")
	}
	// The old "routes" key must no longer be populated.
	if len(reg.Routes) != 0 {
		t.Errorf("routes array has %d entries, want 0 (PR changed to resources)", len(reg.Routes))
	}
}

// TestHandleMethod_ManagementRegister_RelativePath verifies the resource path is relative.
func TestHandleMethod_ManagementRegister_RelativePath(t *testing.T) {
	raw, err := handleMethod("management.register")
	if err != nil {
		t.Fatalf("handleMethod(management.register) error = %v", err)
	}
	_, result, _ := decodeEnvMgmt(t, raw)

	var reg managementRegisterResultMgmt
	if err := json.Unmarshal(result, &reg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(reg.Resources) == 0 {
		t.Fatal("resources array is empty")
	}
	path := reg.Resources[0].Path
	// New path format is relative (starts with /) and does NOT contain the full plugin prefix.
	if !strings.HasPrefix(path, "/") {
		t.Errorf("resource Path = %q, want to start with '/'", path)
	}
	if strings.Contains(path, "/plugins/") {
		t.Errorf("resource Path = %q still contains old /plugins/ prefix", path)
	}
}

// TestHandleMethod_ManagementRegister_MenuAndDescription verifies populated fields.
func TestHandleMethod_ManagementRegister_MenuAndDescription(t *testing.T) {
	raw, err := handleMethod("management.register")
	if err != nil {
		t.Fatalf("handleMethod(management.register) error = %v", err)
	}
	_, result, _ := decodeEnvMgmt(t, raw)

	var reg managementRegisterResultMgmt
	if err := json.Unmarshal(result, &reg); err != nil {
		t.Fatalf("unmarshal: %v", err)
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

// TestHandleMethod_ManagementHandle_ContentTypeIsHTML verifies management.handle now returns
// text/html (changed from application/json in this PR).
func TestHandleMethod_ManagementHandle_ContentTypeIsHTML(t *testing.T) {
	raw, err := handleMethod("management.handle")
	if err != nil {
		t.Fatalf("handleMethod(management.handle) error = %v", err)
	}
	ok, result, _ := decodeEnvMgmt(t, raw)
	if !ok {
		t.Fatalf("handleMethod(management.handle) ok = false, want true")
	}

	var resp managementHandleResultMgmt
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
	if strings.Contains(contentType[0], "application/json") {
		t.Errorf("content-type still contains application/json, want text/html only (PR changed this)")
	}
}

// TestHandleMethod_ManagementHandle_StatusCode200 verifies management.handle returns 200.
func TestHandleMethod_ManagementHandle_StatusCode200(t *testing.T) {
	raw, err := handleMethod("management.handle")
	if err != nil {
		t.Fatalf("handleMethod(management.handle) error = %v", err)
	}
	_, result, _ := decodeEnvMgmt(t, raw)

	var resp managementHandleResultMgmt
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
}

// TestHandleMethod_ManagementHandle_BodyContainsHTML verifies the response body is HTML.
func TestHandleMethod_ManagementHandle_BodyContainsHTML(t *testing.T) {
	raw, err := handleMethod("management.handle")
	if err != nil {
		t.Fatalf("handleMethod(management.handle) error = %v", err)
	}
	_, result, _ := decodeEnvMgmt(t, raw)

	var resp managementHandleResultMgmt
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Body) == 0 {
		t.Fatal("response body is empty")
	}
	// Body may be decoded as []byte by the JSON unmarshaler (base64 decoding).
	bodyStr := string(resp.Body)
	lc := strings.ToLower(bodyStr)
	if !strings.Contains(lc, "<!doctype html>") && !strings.Contains(lc, "<html>") && !strings.Contains(lc, "<title>") {
		// Try base64 decoding in case it wasn't decoded.
		decoded, errDecode := base64.StdEncoding.DecodeString(bodyStr)
		if errDecode == nil {
			lc = strings.ToLower(string(decoded))
		}
		if !strings.Contains(lc, "<!doctype html>") && !strings.Contains(lc, "<title>") {
			t.Errorf("response body does not appear to be HTML: %q", bodyStr[:minLen(len(bodyStr), 200)])
		}
	}
}

// TestHandleMethod_UnknownMethod verifies unknown methods return an error envelope.
func TestHandleMethod_UnknownMethod(t *testing.T) {
	raw, err := handleMethod("not.a.real.method")
	if err != nil {
		t.Fatalf("handleMethod(unknown) returned error = %v, want nil", err)
	}
	ok, _, errInfo := decodeEnvMgmt(t, raw)
	if ok {
		t.Fatalf("handleMethod(unknown) ok = true, want false")
	}
	if errInfo == nil || errInfo.Code != "unknown_method" {
		t.Errorf("error code = %v, want %q", errInfo, "unknown_method")
	}
}

// TestOkEnvelopeJSON verifies that okEnvelopeJSON wraps raw JSON in a success envelope.
func TestOkEnvelopeJSON(t *testing.T) {
	raw, err := okEnvelopeJSON(`{"x":1}`)
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
	if !strings.Contains(string(env.Result), "1") {
		t.Errorf("result does not contain original value: %s", env.Result)
	}
}

// TestErrorEnvelope verifies errorEnvelope builds a proper error envelope.
func TestErrorEnvelope(t *testing.T) {
	raw := errorEnvelope("my_code", "my message")
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.OK {
		t.Errorf("OK = true, want false")
	}
	if env.Error == nil {
		t.Fatal("Error is nil")
	}
	if env.Error.Code != "my_code" {
		t.Errorf("Code = %q, want %q", env.Error.Code, "my_code")
	}
	if env.Error.Message != "my message" {
		t.Errorf("Message = %q, want %q", env.Error.Message, "my message")
	}
}

func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}