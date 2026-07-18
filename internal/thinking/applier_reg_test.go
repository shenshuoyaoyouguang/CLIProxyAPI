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
