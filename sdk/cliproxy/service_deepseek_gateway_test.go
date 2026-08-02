package cliproxy

import (
	"context"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestApplyDeepSeekGatewayReloadPreservesLimiterAccounting(t *testing.T) {
	service := &Service{}

	service.applyDeepSeekGateway(&config.Config{
		DeepSeekGateway: internalconfig.DeepSeekGatewayConfig{
			Enabled:                 true,
			GlobalMaxConcurrency:    2,
			PerUserIDMaxConcurrency: 1,
		},
	})
	firstHook := service.deepSeekGatewayHook
	if firstHook == nil || !firstHook.Enabled() {
		t.Fatal("expected first DeepSeek gateway hook to be enabled")
	}
	firstManager := firstHook.LimiterManager()
	if firstManager == nil {
		t.Fatal("expected first DeepSeek limiter manager")
	}

	release, errAcquire := firstHook.AcquireSlot(context.Background(), "user-a")
	if errAcquire != nil {
		t.Fatalf("AcquireSlot() error = %v", errAcquire)
	}
	defer release()

	service.applyDeepSeekGateway(&config.Config{
		DeepSeekGateway: internalconfig.DeepSeekGatewayConfig{
			Enabled:                 true,
			GlobalMaxConcurrency:    3,
			PerUserIDMaxConcurrency: 2,
		},
	})

	secondHook := service.deepSeekGatewayHook
	if secondHook == nil || !secondHook.Enabled() {
		t.Fatal("expected reloaded DeepSeek gateway hook to be enabled")
	}
	secondManager := secondHook.LimiterManager()
	if secondManager != firstManager {
		t.Fatal("expected DeepSeek limiter manager to be reused across enabled reload")
	}
	if got := secondManager.GlobalSemaphoreCap(); got != 3 {
		t.Fatalf("global semaphore cap = %d, want 3", got)
	}
	stats, ok := secondManager.GetShardStats("user-a")
	if !ok {
		t.Fatal("expected existing shard stats after reload")
	}
	if stats.Active != 1 {
		t.Fatalf("active slots = %d, want 1", stats.Active)
	}
	if stats.MaxConcurrency != 2 {
		t.Fatalf("shard max concurrency = %d, want 2", stats.MaxConcurrency)
	}
}
