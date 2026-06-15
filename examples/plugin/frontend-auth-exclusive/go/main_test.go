package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// decodeEnvelope is a test helper that unmarshals an envelope and returns ok, result bytes, and error info.
func decodeEnvelope(t *testing.T, raw []byte) (bool, json.RawMessage, *envelopeError) {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	return env.OK, env.Result, env.Error
}

// TestHandleMethod_Register verifies that plugin.register and plugin.reconfigure
// return a successful envelope with both frontend auth capabilities set to true.
func TestHandleMethod_Register(t *testing.T) {
	for _, method := range []string{pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure} {
		t.Run(method, func(t *testing.T) {
			raw, err := handleMethod(method, nil)
			if err != nil {
				t.Fatalf("handleMethod(%q) error = %v", method, err)
			}
			ok, result, _ := decodeEnvelope(t, raw)
			if !ok {
				t.Fatalf("handleMethod(%q) ok = false, want true", method)
			}
			var reg registration
			if err := json.Unmarshal(result, &reg); err != nil {
				t.Fatalf("unmarshal registration: %v", err)
			}
			if !reg.Capabilities.FrontendAuthProvider {
				t.Errorf("FrontendAuthProvider = false, want true")
			}
			if !reg.Capabilities.FrontendAuthProviderExclusive {
				t.Errorf("FrontendAuthProviderExclusive = false, want true")
			}
			if reg.SchemaVersion != pluginabi.SchemaVersion {
				t.Errorf("SchemaVersion = %d, want %d", reg.SchemaVersion, pluginabi.SchemaVersion)
			}
		})
	}
}

// TestHandleMethod_Identifier verifies that the identifier method returns the correct plugin ID.
func TestHandleMethod_Identifier(t *testing.T) {
	raw, err := handleMethod(pluginabi.MethodFrontendAuthIdentifier, nil)
	if err != nil {
		t.Fatalf("handleMethod(%q) error = %v", pluginabi.MethodFrontendAuthIdentifier, err)
	}
	ok, result, _ := decodeEnvelope(t, raw)
	if !ok {
		t.Fatalf("handleMethod identifier ok = false, want true")
	}
	var idResp identifierResponse
	if err := json.Unmarshal(result, &idResp); err != nil {
		t.Fatalf("unmarshal identifier response: %v", err)
	}
	const want = "example-frontend-auth-exclusive-go"
	if idResp.Identifier != want {
		t.Errorf("Identifier = %q, want %q", idResp.Identifier, want)
	}
}

// TestHandleMethod_UnknownMethod verifies that unknown methods return an error envelope.
func TestHandleMethod_UnknownMethod(t *testing.T) {
	raw, err := handleMethod("nonexistent.method", nil)
	if err != nil {
		t.Fatalf("handleMethod(unknown) returned error = %v, want nil", err)
	}
	ok, _, errInfo := decodeEnvelope(t, raw)
	if ok {
		t.Fatalf("handleMethod(unknown) ok = true, want false")
	}
	if errInfo == nil {
		t.Fatal("handleMethod(unknown) error info is nil")
	}
	if errInfo.Code != "unknown_method" {
		t.Errorf("error code = %q, want %q", errInfo.Code, "unknown_method")
	}
}

// TestAuthenticate_MissingHeader verifies that a request without the auth header is rejected.
func TestAuthenticate_MissingHeader(t *testing.T) {
	req := pluginapi.FrontendAuthRequest{
		Headers: http.Header{},
	}
	reqBytes, _ := json.Marshal(req)
	raw, err := handleMethod(pluginabi.MethodFrontendAuthAuthenticate, reqBytes)
	if err != nil {
		t.Fatalf("authenticate with missing header returned error = %v", err)
	}
	ok, result, _ := decodeEnvelope(t, raw)
	if !ok {
		t.Fatalf("authenticate ok = false, want true (authentication decisions should not error)")
	}
	var authResp pluginapi.FrontendAuthResponse
	if err := json.Unmarshal(result, &authResp); err != nil {
		t.Fatalf("unmarshal auth response: %v", err)
	}
	if authResp.Authenticated {
		t.Errorf("Authenticated = true with missing header, want false")
	}
}

// TestAuthenticate_WrongHeaderValue verifies that a request with the wrong header value is rejected.
func TestAuthenticate_WrongHeaderValue(t *testing.T) {
	req := pluginapi.FrontendAuthRequest{
		Headers: http.Header{
			"X-Example-Frontend-Auth": []string{"wrong-value"},
		},
	}
	reqBytes, _ := json.Marshal(req)
	raw, err := handleMethod(pluginabi.MethodFrontendAuthAuthenticate, reqBytes)
	if err != nil {
		t.Fatalf("authenticate with wrong header returned error = %v", err)
	}
	ok, result, _ := decodeEnvelope(t, raw)
	if !ok {
		t.Fatalf("authenticate ok = false, want true")
	}
	var authResp pluginapi.FrontendAuthResponse
	if err := json.Unmarshal(result, &authResp); err != nil {
		t.Fatalf("unmarshal auth response: %v", err)
	}
	if authResp.Authenticated {
		t.Errorf("Authenticated = true with wrong header value, want false")
	}
}

// TestAuthenticate_CorrectHeader verifies that a request with the correct header is authenticated.
func TestAuthenticate_CorrectHeader(t *testing.T) {
	req := pluginapi.FrontendAuthRequest{
		Headers: http.Header{
			"X-Example-Frontend-Auth": []string{"exclusive"},
		},
	}
	reqBytes, _ := json.Marshal(req)
	raw, err := handleMethod(pluginabi.MethodFrontendAuthAuthenticate, reqBytes)
	if err != nil {
		t.Fatalf("authenticate with correct header returned error = %v", err)
	}
	ok, result, _ := decodeEnvelope(t, raw)
	if !ok {
		t.Fatalf("authenticate ok = false, want true")
	}
	var authResp pluginapi.FrontendAuthResponse
	if err := json.Unmarshal(result, &authResp); err != nil {
		t.Fatalf("unmarshal auth response: %v", err)
	}
	if !authResp.Authenticated {
		t.Errorf("Authenticated = false with correct header, want true")
	}
	if authResp.Principal != "example-frontend-auth-exclusive-go" {
		t.Errorf("Principal = %q, want %q", authResp.Principal, "example-frontend-auth-exclusive-go")
	}
	if authResp.Metadata["mode"] != "exclusive" {
		t.Errorf("metadata mode = %q, want %q", authResp.Metadata["mode"], "exclusive")
	}
	if authResp.Metadata["provider"] != "example-frontend-auth-exclusive-go" {
		t.Errorf("metadata provider = %q, want %q", authResp.Metadata["provider"], "example-frontend-auth-exclusive-go")
	}
}

// TestAuthenticate_CaseSensitive verifies that the header value check is case-sensitive.
func TestAuthenticate_CaseSensitive(t *testing.T) {
	req := pluginapi.FrontendAuthRequest{
		Headers: http.Header{
			"X-Example-Frontend-Auth": []string{"Exclusive"}, // capital E
		},
	}
	reqBytes, _ := json.Marshal(req)
	raw, err := handleMethod(pluginabi.MethodFrontendAuthAuthenticate, reqBytes)
	if err != nil {
		t.Fatalf("authenticate error = %v", err)
	}
	ok, result, _ := decodeEnvelope(t, raw)
	if !ok {
		t.Fatalf("authenticate ok = false, want true")
	}
	var authResp pluginapi.FrontendAuthResponse
	if err := json.Unmarshal(result, &authResp); err != nil {
		t.Fatalf("unmarshal auth response: %v", err)
	}
	if authResp.Authenticated {
		t.Errorf("Authenticated = true with wrong case, want false")
	}
}

// TestAuthenticate_InvalidJSON verifies that invalid JSON request returns unauthenticated response.
func TestAuthenticate_InvalidJSON(t *testing.T) {
	raw, err := handleMethod(pluginabi.MethodFrontendAuthAuthenticate, []byte("not-json"))
	if err != nil {
		t.Fatalf("authenticate with invalid JSON returned error = %v", err)
	}
	ok, result, _ := decodeEnvelope(t, raw)
	if !ok {
		t.Fatalf("authenticate ok = false, want true")
	}
	var authResp pluginapi.FrontendAuthResponse
	if err := json.Unmarshal(result, &authResp); err != nil {
		t.Fatalf("unmarshal auth response: %v", err)
	}
	if authResp.Authenticated {
		t.Errorf("Authenticated = true with invalid JSON, want false")
	}
}

// TestExampleRegistration verifies the static registration data.
func TestExampleRegistration(t *testing.T) {
	reg := exampleRegistration()
	if reg.Metadata.Name != "example-frontend-auth-exclusive-go" {
		t.Errorf("Name = %q, want %q", reg.Metadata.Name, "example-frontend-auth-exclusive-go")
	}
	if reg.Metadata.Version == "" {
		t.Errorf("Version is empty")
	}
	if !reg.Capabilities.FrontendAuthProvider {
		t.Errorf("FrontendAuthProvider = false, want true")
	}
	if !reg.Capabilities.FrontendAuthProviderExclusive {
		t.Errorf("FrontendAuthProviderExclusive = false, want true")
	}
	if reg.Metadata.ConfigFields == nil {
		t.Errorf("ConfigFields is nil, want empty slice")
	}
}

// TestOkEnvelope verifies that okEnvelope wraps the value in a successful envelope.
func TestOkEnvelope(t *testing.T) {
	type testPayload struct {
		Foo string `json:"foo"`
	}
	raw, err := okEnvelope(testPayload{Foo: "bar"})
	if err != nil {
		t.Fatalf("okEnvelope error = %v", err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if !env.OK {
		t.Errorf("env.OK = false, want true")
	}
	if env.Error != nil {
		t.Errorf("env.Error is set, want nil")
	}
	var result testPayload
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Foo != "bar" {
		t.Errorf("result.Foo = %q, want %q", result.Foo, "bar")
	}
}

// TestErrorEnvelope verifies that errorEnvelope builds a failed envelope with the right code/message.
func TestErrorEnvelope(t *testing.T) {
	raw := errorEnvelope("test_code", "test message")
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.OK {
		t.Errorf("env.OK = true, want false")
	}
	if env.Error == nil {
		t.Fatal("env.Error is nil")
	}
	if env.Error.Code != "test_code" {
		t.Errorf("Code = %q, want %q", env.Error.Code, "test_code")
	}
	if env.Error.Message != "test message" {
		t.Errorf("Message = %q, want %q", env.Error.Message, "test message")
	}
}