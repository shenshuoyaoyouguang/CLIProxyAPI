package thinking

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

type stubApplier struct{}

func (stubApplier) Apply(body []byte, _ ThinkingConfig, _ *registry.ModelInfo) ([]byte, error) {
	return body, nil
}

func (stubApplier) SupportsNativeDisabled() bool { return false }

func TestGetProviderApplierReturnsRegisteredNativeEvenIfNotOnAllowlist(t *testing.T) {
	const name = "reg-test-orphan-provider"
	RegisterProvider(name, stubApplier{})
	t.Cleanup(func() {
		providerAppliersMu.Lock()
		delete(nativeProviderAppliers, name)
		providerAppliersMu.Unlock()
	})

	if got := GetProviderApplier(name); got == nil {
		t.Fatal("GetProviderApplier returned nil for a RegisterProvider result; registered native appliers must remain reachable")
	}
}

func TestNativeProviderAllowlistIncludesInteractions(t *testing.T) {
	if !nativeProviderNames["interactions"] {
		t.Fatal(`nativeProviderNames must include "interactions"`)
	}
}

// TestRegisterPluginProviderBlocksAllowlistedNativeNames verifies that plugins
// cannot claim a built-in provider name that is on the native allowlist, even
// when that name has not yet been registered via RegisterProvider (empty
// nativeProviderAppliers entry). Pre-refactor pre-seeded nil keys enforced this;
// the empty-map redesign must keep the reservation via nativeProviderNames.
func TestRegisterPluginProviderBlocksAllowlistedNativeNames(t *testing.T) {
	// Ensure "claude" is allowlisted but temporarily remove any registered applier
	// so the nativeProviderAppliers key-presence check alone would not block plugins.
	providerAppliersMu.Lock()
	saved, had := nativeProviderAppliers["claude"]
	delete(nativeProviderAppliers, "claude")
	providerAppliersMu.Unlock()
	t.Cleanup(func() {
		providerAppliersMu.Lock()
		if had {
			nativeProviderAppliers["claude"] = saved
		} else {
			delete(nativeProviderAppliers, "claude")
		}
		delete(pluginProviderAppliers, "claude")
		providerAppliersMu.Unlock()
	})

	if !nativeProviderNames["claude"] {
		t.Fatal(`nativeProviderNames must include "claude" for this reservation test`)
	}
	ok := RegisterPluginProvider("evil-plugin", "claude", 100, stubApplier{})
	if ok {
		t.Fatal("RegisterPluginProvider must reject allowlisted native name even when native applier is absent")
	}
	// Rejected registration must not leave a plugin entry that GetProviderApplier would return.
	providerAppliersMu.RLock()
	_, pluginPresent := pluginProviderAppliers["claude"]
	providerAppliersMu.RUnlock()
	if pluginPresent {
		t.Fatal("rejected RegisterPluginProvider must not leave a plugin applier for the native name")
	}
}

// TestBuildRegisteredEffort_NilSupportNoPanic verifies the public helper does
// not panic when support is nil on non-ModeLevel paths (ModeNone/Budget fallback).
func TestBuildRegisteredEffort_NilSupportNoPanic(t *testing.T) {
	body := []byte(`{"messages":[]}`)
	out, err := BuildRegisteredEffort(body, ThinkingConfig{Mode: ModeNone, Budget: 0}, nil, "reasoning_effort")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != string(body) {
		t.Fatalf("nil support ModeNone must passthrough; got %s", out)
	}
}
