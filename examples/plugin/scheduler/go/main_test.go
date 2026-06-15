package main

import (
	"encoding/json"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// decodeEnvSched is a test helper that unmarshals an envelope for the scheduler plugin.
func decodeEnvSched(t *testing.T, raw []byte) (bool, json.RawMessage, *envelopeError) {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	return env.OK, env.Result, env.Error
}

// resetConfig clears the current configuration to the zero value between tests.
func resetConfig() {
	currentConfig.Store(pluginConfig{})
}

// TestDecodeConfig_Empty verifies that empty YAML produces a zero-value config.
func TestDecodeConfig_Empty(t *testing.T) {
	cfg, err := decodeConfig([]byte(""))
	if err != nil {
		t.Fatalf("decodeConfig empty: %v", err)
	}
	if cfg.AuthID != "" || cfg.Delegate != "" || cfg.Deny {
		t.Errorf("decodeConfig empty: got non-zero config %+v", cfg)
	}
}

// TestDecodeConfig_AllFields verifies all YAML fields are decoded correctly.
func TestDecodeConfig_AllFields(t *testing.T) {
	yaml := []byte("auth_id: test-auth\ndelegate: fill-first\ndeny: true\n")
	cfg, err := decodeConfig(yaml)
	if err != nil {
		t.Fatalf("decodeConfig: %v", err)
	}
	if cfg.AuthID != "test-auth" {
		t.Errorf("AuthID = %q, want %q", cfg.AuthID, "test-auth")
	}
	if cfg.Delegate != "fill-first" {
		t.Errorf("Delegate = %q, want %q", cfg.Delegate, "fill-first")
	}
	if !cfg.Deny {
		t.Errorf("Deny = false, want true")
	}
}

// TestDecodeConfig_InvalidYAML verifies that invalid YAML returns an error.
func TestDecodeConfig_InvalidYAML(t *testing.T) {
	_, err := decodeConfig([]byte(":\tinvalid:yaml:"))
	if err == nil {
		t.Fatal("decodeConfig with invalid YAML should return error")
	}
}

// TestConfigure_EmptyRequest verifies that an empty request does not fail and produces defaults.
func TestConfigure_EmptyRequest(t *testing.T) {
	defer resetConfig()
	if err := configure(nil); err != nil {
		t.Fatalf("configure(nil) error = %v", err)
	}
	cfg := loadedConfig()
	if cfg.AuthID != "" || cfg.Delegate != "" || cfg.Deny {
		t.Errorf("configure(nil) stored non-zero config %+v", cfg)
	}
}

// TestConfigure_WithYAML verifies that a lifecycle request with config YAML is applied.
func TestConfigure_WithYAML(t *testing.T) {
	defer resetConfig()
	req := lifecycleRequest{
		ConfigYAML: []byte("auth_id: myauth\n"),
	}
	raw, _ := json.Marshal(req)
	if err := configure(raw); err != nil {
		t.Fatalf("configure error = %v", err)
	}
	cfg := loadedConfig()
	if cfg.AuthID != "myauth" {
		t.Errorf("AuthID = %q, want %q", cfg.AuthID, "myauth")
	}
}

// TestConfigure_TrimsWhitespace verifies that auth_id and delegate are trimmed.
func TestConfigure_TrimsWhitespace(t *testing.T) {
	defer resetConfig()
	req := lifecycleRequest{
		ConfigYAML: []byte("auth_id: '  spaced  '\ndelegate: '  round-robin  '\n"),
	}
	raw, _ := json.Marshal(req)
	if err := configure(raw); err != nil {
		t.Fatalf("configure error = %v", err)
	}
	cfg := loadedConfig()
	if cfg.AuthID != "spaced" {
		t.Errorf("AuthID = %q, want %q", cfg.AuthID, "spaced")
	}
	if cfg.Delegate != "round-robin" {
		t.Errorf("Delegate = %q, want %q", cfg.Delegate, "round-robin")
	}
}

// TestConfigure_InvalidJSON verifies that invalid JSON request returns an error.
func TestConfigure_InvalidJSON(t *testing.T) {
	defer resetConfig()
	if err := configure([]byte("not json")); err == nil {
		t.Fatal("configure with invalid JSON should return error")
	}
}

// TestLoadedConfig_Default verifies that loadedConfig returns zero value when nothing is stored.
func TestLoadedConfig_Default(t *testing.T) {
	defer resetConfig()
	resetConfig()
	cfg := loadedConfig()
	if cfg.AuthID != "" || cfg.Delegate != "" || cfg.Deny {
		t.Errorf("loadedConfig default: got non-zero %+v", cfg)
	}
}

// TestHandleMethod_Register verifies registration returns the scheduler capability.
func TestHandleMethod_Register(t *testing.T) {
	defer resetConfig()
	for _, method := range []string{pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure} {
		t.Run(method, func(t *testing.T) {
			raw, err := handleMethod(method, nil)
			if err != nil {
				t.Fatalf("handleMethod(%q) error = %v", method, err)
			}
			ok, result, _ := decodeEnvSched(t, raw)
			if !ok {
				t.Fatalf("handleMethod(%q) ok = false, want true", method)
			}
			var reg registration
			if err := json.Unmarshal(result, &reg); err != nil {
				t.Fatalf("unmarshal registration: %v", err)
			}
			if !reg.Capabilities.Scheduler {
				t.Errorf("Scheduler capability = false, want true")
			}
			if reg.SchemaVersion != pluginabi.SchemaVersion {
				t.Errorf("SchemaVersion = %d, want %d", reg.SchemaVersion, pluginabi.SchemaVersion)
			}
		})
	}
}

// TestHandleMethod_UnknownMethod verifies unknown methods return an error envelope.
func TestHandleMethod_UnknownMethod(t *testing.T) {
	raw, err := handleMethod("unknown.method", nil)
	if err != nil {
		t.Fatalf("handleMethod(unknown) returned error = %v, want nil", err)
	}
	ok, _, errInfo := decodeEnvSched(t, raw)
	if ok {
		t.Fatalf("handleMethod(unknown) ok = true, want false")
	}
	if errInfo == nil || errInfo.Code != "unknown_method" {
		t.Errorf("error code = %v, want %q", errInfo, "unknown_method")
	}
}

// TestPickAuth_Deny verifies that deny=true returns an error envelope with scheduler_denied code.
func TestPickAuth_Deny(t *testing.T) {
	defer resetConfig()
	currentConfig.Store(pluginConfig{Deny: true})

	req := pluginapi.SchedulerPickRequest{
		Candidates: []pluginapi.SchedulerCandidate{{ID: "auth1"}},
	}
	reqBytes, _ := json.Marshal(req)
	raw, err := pickAuth(reqBytes)
	if err != nil {
		t.Fatalf("pickAuth error = %v", err)
	}
	ok, _, errInfo := decodeEnvSched(t, raw)
	if ok {
		t.Fatalf("pickAuth deny: ok = true, want false")
	}
	if errInfo == nil || errInfo.Code != "scheduler_denied" {
		t.Errorf("error code = %v, want %q", errInfo, "scheduler_denied")
	}
}

// TestPickAuth_DelegateFillFirst verifies that delegate=fill-first returns DelegateBuiltin and Handled=true.
func TestPickAuth_DelegateFillFirst(t *testing.T) {
	defer resetConfig()
	currentConfig.Store(pluginConfig{Delegate: pluginapi.SchedulerBuiltinFillFirst})

	req := pluginapi.SchedulerPickRequest{
		Candidates: []pluginapi.SchedulerCandidate{{ID: "a"}, {ID: "b"}},
	}
	reqBytes, _ := json.Marshal(req)
	raw, err := pickAuth(reqBytes)
	if err != nil {
		t.Fatalf("pickAuth error = %v", err)
	}
	ok, result, _ := decodeEnvSched(t, raw)
	if !ok {
		t.Fatalf("pickAuth fill-first: ok = false, want true")
	}
	var resp pluginapi.SchedulerPickResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal SchedulerPickResponse: %v", err)
	}
	if resp.DelegateBuiltin != pluginapi.SchedulerBuiltinFillFirst {
		t.Errorf("DelegateBuiltin = %q, want %q", resp.DelegateBuiltin, pluginapi.SchedulerBuiltinFillFirst)
	}
	if !resp.Handled {
		t.Errorf("Handled = false, want true")
	}
}

// TestPickAuth_DelegateRoundRobin verifies that delegate=round-robin returns the correct DelegateBuiltin.
func TestPickAuth_DelegateRoundRobin(t *testing.T) {
	defer resetConfig()
	currentConfig.Store(pluginConfig{Delegate: pluginapi.SchedulerBuiltinRoundRobin})

	req := pluginapi.SchedulerPickRequest{
		Candidates: []pluginapi.SchedulerCandidate{{ID: "a"}},
	}
	reqBytes, _ := json.Marshal(req)
	raw, err := pickAuth(reqBytes)
	if err != nil {
		t.Fatalf("pickAuth error = %v", err)
	}
	ok, result, _ := decodeEnvSched(t, raw)
	if !ok {
		t.Fatalf("pickAuth round-robin: ok = false, want true")
	}
	var resp pluginapi.SchedulerPickResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal SchedulerPickResponse: %v", err)
	}
	if resp.DelegateBuiltin != pluginapi.SchedulerBuiltinRoundRobin {
		t.Errorf("DelegateBuiltin = %q, want %q", resp.DelegateBuiltin, pluginapi.SchedulerBuiltinRoundRobin)
	}
	if !resp.Handled {
		t.Errorf("Handled = false, want true")
	}
}

// TestPickAuth_DelegateUnknownValue verifies that an unrecognized delegate value leaves the pick unhandled.
func TestPickAuth_DelegateUnknownValue(t *testing.T) {
	defer resetConfig()
	currentConfig.Store(pluginConfig{Delegate: "unknown-scheduler"})

	req := pluginapi.SchedulerPickRequest{
		Candidates: []pluginapi.SchedulerCandidate{{ID: "a"}},
	}
	reqBytes, _ := json.Marshal(req)
	raw, err := pickAuth(reqBytes)
	if err != nil {
		t.Fatalf("pickAuth error = %v", err)
	}
	ok, result, _ := decodeEnvSched(t, raw)
	if !ok {
		t.Fatalf("pickAuth unknown delegate: ok = false, want true")
	}
	var resp pluginapi.SchedulerPickResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal SchedulerPickResponse: %v", err)
	}
	if resp.Handled {
		t.Errorf("Handled = true for unknown delegate, want false")
	}
}

// TestPickAuth_AuthIDFound verifies that when auth_id matches a candidate, it is returned as picked.
func TestPickAuth_AuthIDFound(t *testing.T) {
	defer resetConfig()
	currentConfig.Store(pluginConfig{AuthID: "target-auth"})

	req := pluginapi.SchedulerPickRequest{
		Candidates: []pluginapi.SchedulerCandidate{
			{ID: "other-auth"},
			{ID: "target-auth"},
			{ID: "another-auth"},
		},
	}
	reqBytes, _ := json.Marshal(req)
	raw, err := pickAuth(reqBytes)
	if err != nil {
		t.Fatalf("pickAuth error = %v", err)
	}
	ok, result, _ := decodeEnvSched(t, raw)
	if !ok {
		t.Fatalf("pickAuth auth_id found: ok = false, want true")
	}
	var resp pluginapi.SchedulerPickResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal SchedulerPickResponse: %v", err)
	}
	if resp.AuthID != "target-auth" {
		t.Errorf("AuthID = %q, want %q", resp.AuthID, "target-auth")
	}
	if !resp.Handled {
		t.Errorf("Handled = false, want true")
	}
}

// TestPickAuth_AuthIDNotInCandidates verifies that when auth_id does not match any candidate, the pick is unhandled.
func TestPickAuth_AuthIDNotInCandidates(t *testing.T) {
	defer resetConfig()
	currentConfig.Store(pluginConfig{AuthID: "missing-auth"})

	req := pluginapi.SchedulerPickRequest{
		Candidates: []pluginapi.SchedulerCandidate{
			{ID: "auth1"},
			{ID: "auth2"},
		},
	}
	reqBytes, _ := json.Marshal(req)
	raw, err := pickAuth(reqBytes)
	if err != nil {
		t.Fatalf("pickAuth error = %v", err)
	}
	ok, result, _ := decodeEnvSched(t, raw)
	if !ok {
		t.Fatalf("pickAuth auth_id not found: ok = false, want true")
	}
	var resp pluginapi.SchedulerPickResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal SchedulerPickResponse: %v", err)
	}
	if resp.Handled {
		t.Errorf("Handled = true when auth_id not in candidates, want false")
	}
}

// TestPickAuth_EmptyAuthIDAndDelegate verifies that with no config, the pick is unhandled.
func TestPickAuth_EmptyAuthIDAndDelegate(t *testing.T) {
	defer resetConfig()
	currentConfig.Store(pluginConfig{})

	req := pluginapi.SchedulerPickRequest{
		Candidates: []pluginapi.SchedulerCandidate{{ID: "auth1"}},
	}
	reqBytes, _ := json.Marshal(req)
	raw, err := pickAuth(reqBytes)
	if err != nil {
		t.Fatalf("pickAuth error = %v", err)
	}
	ok, result, _ := decodeEnvSched(t, raw)
	if !ok {
		t.Fatalf("pickAuth empty config: ok = false, want true")
	}
	var resp pluginapi.SchedulerPickResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal SchedulerPickResponse: %v", err)
	}
	if resp.Handled {
		t.Errorf("Handled = true with empty config, want false")
	}
}

// TestPickAuth_EmptyCandidates verifies that with no candidates, the pick is unhandled.
func TestPickAuth_EmptyCandidates(t *testing.T) {
	defer resetConfig()
	currentConfig.Store(pluginConfig{AuthID: "target-auth"})

	req := pluginapi.SchedulerPickRequest{
		Candidates: []pluginapi.SchedulerCandidate{},
	}
	reqBytes, _ := json.Marshal(req)
	raw, err := pickAuth(reqBytes)
	if err != nil {
		t.Fatalf("pickAuth error = %v", err)
	}
	ok, result, _ := decodeEnvSched(t, raw)
	if !ok {
		t.Fatalf("pickAuth empty candidates: ok = false, want true")
	}
	var resp pluginapi.SchedulerPickResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal SchedulerPickResponse: %v", err)
	}
	if resp.Handled {
		t.Errorf("Handled = true with empty candidates, want false")
	}
}

// TestPickAuth_InvalidJSON verifies that invalid JSON returns an error.
func TestPickAuth_InvalidJSON(t *testing.T) {
	defer resetConfig()
	_, err := pickAuth([]byte("not json"))
	if err == nil {
		t.Fatal("pickAuth with invalid JSON should return error")
	}
}

// TestPickAuth_DenyTakesPrecedenceOverDelegate verifies deny takes precedence.
func TestPickAuth_DenyTakesPrecedenceOverDelegate(t *testing.T) {
	defer resetConfig()
	currentConfig.Store(pluginConfig{
		Deny:     true,
		Delegate: pluginapi.SchedulerBuiltinFillFirst,
		AuthID:   "some-auth",
	})

	req := pluginapi.SchedulerPickRequest{
		Candidates: []pluginapi.SchedulerCandidate{{ID: "some-auth"}},
	}
	reqBytes, _ := json.Marshal(req)
	raw, err := pickAuth(reqBytes)
	if err != nil {
		t.Fatalf("pickAuth error = %v", err)
	}
	ok, _, errInfo := decodeEnvSched(t, raw)
	if ok {
		t.Fatalf("pickAuth deny+delegate: ok = true, want false (deny takes precedence)")
	}
	if errInfo == nil || errInfo.Code != "scheduler_denied" {
		t.Errorf("error code = %v, want %q", errInfo, "scheduler_denied")
	}
}

// TestPluginRegistration_Fields verifies the scheduler registration metadata.
func TestPluginRegistration_Fields(t *testing.T) {
	reg := pluginRegistration()
	if reg.Metadata.Name != "scheduler" {
		t.Errorf("Name = %q, want %q", reg.Metadata.Name, "scheduler")
	}
	if !reg.Capabilities.Scheduler {
		t.Errorf("Scheduler = false, want true")
	}
	// ConfigFields should have auth_id, delegate, deny entries.
	fieldNames := make(map[string]bool)
	for _, f := range reg.Metadata.ConfigFields {
		fieldNames[f.Name] = true
	}
	for _, expected := range []string{"auth_id", "delegate", "deny"} {
		if !fieldNames[expected] {
			t.Errorf("ConfigFields missing %q", expected)
		}
	}
}

// TestHandleMethod_SchedulerPick_ViaHandleMethod verifies scheduler.pick is dispatched through handleMethod.
func TestHandleMethod_SchedulerPick_ViaHandleMethod(t *testing.T) {
	defer resetConfig()
	currentConfig.Store(pluginConfig{Deny: true})

	req := pluginapi.SchedulerPickRequest{
		Candidates: []pluginapi.SchedulerCandidate{{ID: "x"}},
	}
	reqBytes, _ := json.Marshal(req)
	raw, err := handleMethod(pluginabi.MethodSchedulerPick, reqBytes)
	if err != nil {
		t.Fatalf("handleMethod(scheduler.pick) error = %v", err)
	}
	ok, _, errInfo := decodeEnvSched(t, raw)
	if ok {
		t.Fatalf("handleMethod(scheduler.pick) with deny: ok = true, want false")
	}
	if errInfo == nil || errInfo.Code != "scheduler_denied" {
		t.Errorf("error code = %v, want %q", errInfo, "scheduler_denied")
	}
}

// TestOkEnvelope verifies okEnvelope wraps values correctly.
func TestOkEnvelope(t *testing.T) {
	type payload struct{ N int }
	raw, err := okEnvelope(payload{N: 7})
	if err != nil {
		t.Fatalf("okEnvelope error = %v", err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.OK {
		t.Errorf("OK = false, want true")
	}
}

// TestErrorEnvelope verifies errorEnvelope builds a failed envelope.
func TestErrorEnvelope(t *testing.T) {
	raw := errorEnvelope("e_code", "e message")
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
	if env.Error.Code != "e_code" {
		t.Errorf("Code = %q, want %q", env.Error.Code, "e_code")
	}
	if env.Error.Message != "e message" {
		t.Errorf("Message = %q, want %q", env.Error.Message, "e message")
	}
}
