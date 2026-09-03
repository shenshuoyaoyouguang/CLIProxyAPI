package auth

import (
	"context"
	"fmt"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

// benchAuthScale returns the auth counts used to compare the two selection paths.
var benchAuthScale = []int{10, 50, 100, 500, 1000}

const (
	benchProvider = "bench-gemini"
	benchModel    = "bench-model"
)

// registerBenchAuths registers n auths against the global model registry and
// returns them as a slice. Cleanup is the caller's responsibility.
func registerBenchAuths(prefix string, n int) []*Auth {
	reg := registry.GetGlobalRegistry()
	auths := make([]*Auth, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%s-auth-%d", prefix, i)
		reg.RegisterClient(id, benchProvider, []*registry.ModelInfo{{ID: benchModel}})
		auths = append(auths, &Auth{
			ID:         id,
			Provider:   benchProvider,
			Status:     StatusActive,
			Attributes: map[string]string{},
		})
	}
	return auths
}

func unregisterBenchAuths(auths []*Auth) {
	reg := registry.GetGlobalRegistry()
	for _, auth := range auths {
		reg.UnregisterClient(auth.ID)
	}
}

// BenchmarkPickSingle compares the indexed authScheduler against the scan-based
// selector at several auth counts. It exists to answer one question: if the
// scheduler is deleted and every pick becomes a full scan of m.auths, how much
// does a pick actually cost? Run before touching scheduler.go.
//
//	go test ./sdk/cliproxy/auth/ -run '^$' -bench BenchmarkPickSingle -benchmem
func BenchmarkPickSingle(b *testing.B) {
	ctx := context.Background()
	opts := cliproxyexecutor.Options{}

	for _, n := range benchAuthScale {
		// Baseline: the incremental index maintained by authScheduler.
		b.Run(fmt.Sprintf("scheduler/N=%d", n), func(b *testing.B) {
			auths := registerBenchAuths("sched", n)
			defer unregisterBenchAuths(auths)
			scheduler := newAuthScheduler(&RoundRobinSelector{})
			scheduler.rebuild(auths)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				picked, errPick := scheduler.pickSingle(ctx, benchProvider, benchModel, opts, nil)
				if errPick != nil {
					b.Fatalf("pickSingle() error = %v", errPick)
				}
				if picked == nil {
					b.Fatal("pickSingle() returned nil auth")
				}
			}
		})

		// Candidate: scan the auth slice through the selector directly.
		b.Run(fmt.Sprintf("selector/N=%d", n), func(b *testing.B) {
			auths := registerBenchAuths("sel", n)
			defer unregisterBenchAuths(auths)
			selector := &RoundRobinSelector{}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				picked, errPick := selector.Pick(ctx, benchProvider, benchModel, opts, auths)
				if errPick != nil {
					b.Fatalf("Pick() error = %v", errPick)
				}
				if picked == nil {
					b.Fatal("Pick() returned nil auth")
				}
			}
		})
	}
}
