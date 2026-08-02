package auth

import (
	"context"
	"sync"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

// schedulerStoredAuth returns the *Auth pointer the scheduler currently
// references for authID, or nil when it has no entry for that auth.
func schedulerStoredAuth(m *Manager, authID string) *Auth {
	if m == nil || m.scheduler == nil {
		return nil
	}
	m.scheduler.mu.Lock()
	defer m.scheduler.mu.Unlock()
	for _, providerState := range m.scheduler.providers {
		if providerState == nil {
			continue
		}
		if meta := providerState.auths[authID]; meta != nil {
			return meta.auth
		}
	}
	return nil
}

// TestManager_RegisterUpdate_DoNotAliasSchedulerAuth guards the invariant that
// the scheduler never stores the live m.auths object. Scheduler picks read
// entry.auth.ModelStates under scheduler.mu, while MarkResult mutates the same
// map in place under m.mu; aliasing the object would crash the process with
// "concurrent map read and map write". Register and Update must hand the
// scheduler an independent clone instead.
func TestManager_RegisterUpdate_DoNotAliasSchedulerAuth(t *testing.T) {
	m := NewManager(nil, nil, nil)

	auth := &Auth{ID: "auth-alias", Provider: "claude", Status: StatusActive}
	if _, errRegister := m.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	m.mu.RLock()
	live := m.auths[auth.ID]
	m.mu.RUnlock()
	if stored := schedulerStoredAuth(m, auth.ID); stored == nil {
		t.Fatalf("scheduler has no entry for auth after Register")
	} else if live == stored {
		t.Fatalf("Register handed the live m.auths pointer to the scheduler")
	}

	if _, errUpdate := m.Update(context.Background(), &Auth{ID: "auth-alias", Provider: "claude", Status: StatusActive}); errUpdate != nil {
		t.Fatalf("update auth: %v", errUpdate)
	}

	m.mu.RLock()
	live = m.auths["auth-alias"]
	m.mu.RUnlock()
	if stored := schedulerStoredAuth(m, "auth-alias"); stored == nil {
		t.Fatalf("scheduler has no entry for auth after Update")
	} else if live == stored {
		t.Fatalf("Update handed the live m.auths pointer to the scheduler")
	}
}

// TestManager_ConcurrentUpdatePickMarkResult_NoSchedulerAliasRace is a stress
// test that exercises the C2 crash scenario: Register/Update run concurrently
// with scheduler picks and MarkResult-style in-place ModelStates mutation. Run
// with -race (or plain) it must not fatal with "concurrent map read and map
// write"; before the clone fix the scheduler aliased the live m.auths object,
// so scheduler picks (scheduler.mu) and MarkResult (m.mu) hit the same map.
func TestManager_ConcurrentUpdatePickMarkResult_NoSchedulerAliasRace(t *testing.T) {
	m := NewManager(nil, nil, nil)

	auth := &Auth{ID: "auth-race", Provider: "claude", Status: StatusActive}
	if _, errRegister := m.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	const (
		updaterCount = 2
		markerCount  = 4
		pickerCount  = 4
		runDuration  = 200 * time.Millisecond
	)

	start := make(chan struct{})
	stop := make(chan struct{})
	var wg sync.WaitGroup

	run := func(body func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			body()
		}()
	}

	for i := 0; i < updaterCount; i++ {
		run(func() {
			seq := 0
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, errUpdate := m.Update(context.Background(), &Auth{
					ID:       "auth-race",
					Provider: "claude",
					Status:   StatusActive,
					Metadata: map[string]any{"seq": seq},
				}); errUpdate != nil {
					t.Errorf("update auth: %v", errUpdate)
					return
				}
				seq++
			}
		})
	}

	for i := 0; i < markerCount/2; i++ {
		run(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				m.MarkResult(context.Background(), Result{AuthID: "auth-race", Model: "model-x", Success: true})
			}
		})
		run(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				m.MarkResult(context.Background(), Result{
					AuthID:  "auth-race",
					Model:   "model-x",
					Success: false,
					Error:   &Error{Message: "transient upstream error", HTTPStatus: 500},
				})
			}
		})
	}

	for i := 0; i < pickerCount; i++ {
		run(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				// An empty model key builds the shard from every provider auth and
				// exercises isAuthBlockedForModel reads of entry.auth.ModelStates.
				// Transient "no auth available" errors are expected while MarkResult
				// temporarily blocks the auth.
				_, _ = m.scheduler.pickSingle(context.Background(), "claude", "", cliproxyexecutor.Options{}, nil)
			}
		})
	}

	close(start)
	time.Sleep(runDuration)
	close(stop)
	wg.Wait()
}
