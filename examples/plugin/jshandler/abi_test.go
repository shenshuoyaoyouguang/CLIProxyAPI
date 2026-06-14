package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestABIRegistrationUsesHostStreamCapabilityField(t *testing.T) {
	raw, errMarshal := abiOKEnvelope(abiRegistration{
		Capabilities: abiCapabilities{
			RequestInterceptor:     true,
			ResponseInterceptor:    true,
			StreamChunkInterceptor: true,
		},
	})
	if errMarshal != nil {
		t.Fatalf("abiOKEnvelope() error = %v", errMarshal)
	}

	var envelope abiEnvelope
	if errUnmarshal := json.Unmarshal(raw, &envelope); errUnmarshal != nil {
		t.Fatalf("json.Unmarshal(envelope) error = %v", errUnmarshal)
	}
	var result struct {
		Capabilities map[string]bool `json:"capabilities"`
	}
	if errUnmarshal := json.Unmarshal(envelope.Result, &result); errUnmarshal != nil {
		t.Fatalf("json.Unmarshal(result) error = %v", errUnmarshal)
	}
	if !result.Capabilities["response_stream_interceptor"] {
		t.Fatalf("response_stream_interceptor capability was not advertised: %v", result.Capabilities)
	}
	if _, exists := result.Capabilities["stream_chunk_interceptor"]; exists {
		t.Fatalf("legacy stream_chunk_interceptor field should not be advertised: %v", result.Capabilities)
	}
}

// abiOKEnvelope tests

func TestABIEnvelopeOKIsTrue(t *testing.T) {
	raw, err := abiOKEnvelope("hello")
	if err != nil {
		t.Fatalf("abiOKEnvelope() error = %v", err)
	}
	var env abiEnvelope
	if errU := json.Unmarshal(raw, &env); errU != nil {
		t.Fatalf("json.Unmarshal() error = %v", errU)
	}
	if !env.OK {
		t.Fatal("abiOKEnvelope() ok = false, want true")
	}
	if env.Error != nil {
		t.Fatalf("abiOKEnvelope() error field = %v, want nil", env.Error)
	}
}

func TestABIEnvelopeErrorIsNotOK(t *testing.T) {
	raw := abiErrorEnvelope("test_code", "test message")
	var env abiEnvelope
	if errU := json.Unmarshal(raw, &env); errU != nil {
		t.Fatalf("json.Unmarshal() error = %v", errU)
	}
	if env.OK {
		t.Fatal("abiErrorEnvelope() ok = true, want false")
	}
	if env.Error == nil {
		t.Fatal("abiErrorEnvelope() error = nil, want non-nil")
	}
	if env.Error.Code != "test_code" {
		t.Fatalf("abiErrorEnvelope() code = %q, want test_code", env.Error.Code)
	}
	if env.Error.Message != "test message" {
		t.Fatalf("abiErrorEnvelope() message = %q, want test message", env.Error.Message)
	}
}

func TestABIErrorEnvelopeHasNoResult(t *testing.T) {
	raw := abiErrorEnvelope("code", "msg")
	var env abiEnvelope
	if errU := json.Unmarshal(raw, &env); errU != nil {
		t.Fatalf("json.Unmarshal() error = %v", errU)
	}
	if len(env.Result) > 0 && string(env.Result) != "null" {
		t.Fatalf("abiErrorEnvelope() result = %s, want empty/null", env.Result)
	}
}

func TestABIOKEnvelopeWithErrorReturnsErrorWhenErrSet(t *testing.T) {
	inputErr := errors.New("something failed")
	_, err := abiOKEnvelopeWithError("value", inputErr)
	if err == nil {
		t.Fatal("abiOKEnvelopeWithError() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "something failed") {
		t.Fatalf("abiOKEnvelopeWithError() error = %v, want 'something failed'", err)
	}
}

func TestABIOKEnvelopeWithErrorReturnsOKWhenErrNil(t *testing.T) {
	raw, err := abiOKEnvelopeWithError("value", nil)
	if err != nil {
		t.Fatalf("abiOKEnvelopeWithError(nil) error = %v", err)
	}
	var env abiEnvelope
	if errU := json.Unmarshal(raw, &env); errU != nil {
		t.Fatalf("json.Unmarshal() error = %v", errU)
	}
	if !env.OK {
		t.Fatal("abiOKEnvelopeWithError(nil err) ok = false, want true")
	}
}

// beginJSHandlerPluginCall tests

func TestBeginJSHandlerPluginCallReturnsErrorWhenNilPlugin(t *testing.T) {
	orig := jsHandlerABIState.plugin
	origShutting := jsHandlerABIState.shuttingDown
	defer func() {
		jsHandlerABIState.Lock()
		jsHandlerABIState.plugin = orig
		jsHandlerABIState.shuttingDown = origShutting
		jsHandlerABIState.Unlock()
	}()

	jsHandlerABIState.Lock()
	jsHandlerABIState.plugin = nil
	jsHandlerABIState.shuttingDown = false
	jsHandlerABIState.Unlock()

	_, _, err := beginJSHandlerPluginCall()
	if err == nil {
		t.Fatal("beginJSHandlerPluginCall() expected error when plugin is nil")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("beginJSHandlerPluginCall() error = %v, want 'not registered'", err)
	}
}

func TestBeginJSHandlerPluginCallReturnsErrorWhenShuttingDown(t *testing.T) {
	orig := jsHandlerABIState.plugin
	origShutting := jsHandlerABIState.shuttingDown
	defer func() {
		jsHandlerABIState.Lock()
		jsHandlerABIState.plugin = orig
		jsHandlerABIState.shuttingDown = origShutting
		jsHandlerABIState.Unlock()
	}()

	jsHandlerABIState.Lock()
	jsHandlerABIState.plugin = &jsHandlerPlugin{}
	jsHandlerABIState.shuttingDown = true
	jsHandlerABIState.Unlock()

	_, _, err := beginJSHandlerPluginCall()
	if err == nil {
		t.Fatal("beginJSHandlerPluginCall() expected error when shutting down")
	}
	if !strings.Contains(err.Error(), "shutting down") {
		t.Fatalf("beginJSHandlerPluginCall() error = %v, want 'shutting down'", err)
	}
}

func TestBeginJSHandlerPluginCallReturnsPluginWhenRegistered(t *testing.T) {
	orig := jsHandlerABIState.plugin
	origShutting := jsHandlerABIState.shuttingDown
	defer func() {
		jsHandlerABIState.Lock()
		jsHandlerABIState.plugin = orig
		jsHandlerABIState.shuttingDown = origShutting
		jsHandlerABIState.Unlock()
	}()

	want := &jsHandlerPlugin{}
	jsHandlerABIState.Lock()
	jsHandlerABIState.plugin = want
	jsHandlerABIState.shuttingDown = false
	jsHandlerABIState.Unlock()

	got, done, err := beginJSHandlerPluginCall()
	if err != nil {
		t.Fatalf("beginJSHandlerPluginCall() error = %v", err)
	}
	done()
	if got != want {
		t.Fatalf("beginJSHandlerPluginCall() returned wrong plugin pointer")
	}
}

// handleJSHandlerABIMethod unknown method test

func TestHandleJSHandlerABIMethodUnknownMethodReturnsErrorEnvelope(t *testing.T) {
	orig := jsHandlerABIState.plugin
	origShutting := jsHandlerABIState.shuttingDown
	defer func() {
		jsHandlerABIState.Lock()
		jsHandlerABIState.plugin = orig
		jsHandlerABIState.shuttingDown = origShutting
		jsHandlerABIState.Unlock()
	}()

	jsHandlerABIState.Lock()
	jsHandlerABIState.plugin = &jsHandlerPlugin{cfg: defaultJSHandlerConfig()}
	jsHandlerABIState.shuttingDown = false
	jsHandlerABIState.Unlock()

	ctx := context.Background()
	raw, err := handleJSHandlerABIMethod(ctx, "unknown.method", nil)
	if err != nil {
		t.Fatalf("handleJSHandlerABIMethod(unknown) error = %v", err)
	}
	var env abiEnvelope
	if errU := json.Unmarshal(raw, &env); errU != nil {
		t.Fatalf("json.Unmarshal() error = %v", errU)
	}
	if env.OK {
		t.Fatal("handleJSHandlerABIMethod(unknown) ok = true, want false")
	}
	if env.Error == nil || env.Error.Code != "unknown_method" {
		t.Fatalf("handleJSHandlerABIMethod(unknown) error = %v, want unknown_method", env.Error)
	}
}
