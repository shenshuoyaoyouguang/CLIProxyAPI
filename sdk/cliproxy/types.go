// Package cliproxy provides the core service implementation for the CLI Proxy API.
// It includes service lifecycle management, authentication handling, file watching,
// and integration with various AI service providers through a unified interface.
package cliproxy

import (
	"context"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/watcher"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// WatcherFactory creates a watcher for configuration and token changes.
// The reload callback receives the updated configuration when changes are detected.
//
// Parameters:
//   - configPath: The path to the configuration file to watch
//   - authDir: The directory containing authentication tokens to watch
//   - reload: The callback function to call when changes are detected
//
// Returns:
//   - *WatcherWrapper: A watcher wrapper instance
//   - error: An error if watcher creation fails
type WatcherFactory func(configPath, authDir string, reload func(*config.Config)) (*WatcherWrapper, error)

// PluginAuthParser parses auth JSON owned by plugin providers.
type PluginAuthParser interface {
	ParseAuth(context.Context, pluginapi.AuthParseRequest) (*coreauth.Auth, bool, error)
}

// WatcherWrapper exposes the subset of watcher methods required by the SDK.
type WatcherWrapper struct {
	start func(ctx context.Context) error
	stop  func() error

	setConfig             func(cfg *config.Config)
	snapshotAuths         func() []*coreauth.Auth
	setUpdateQueue        func(queue chan<- watcher.AuthUpdate)
	dispatchRuntimeUpdate func(update watcher.AuthUpdate) bool
	dispatchPersistedAuth func(update watcher.AuthUpdate) bool
	setPluginAuthParser   func(parser PluginAuthParser)
	reloadConfigIfChanged func()
}

// Start proxies to the underlying watcher Start implementation.
func (w *WatcherWrapper) Start(ctx context.Context) error {
	if w == nil || w.start == nil {
		return nil
	}
	return w.start(ctx)
}

// Stop proxies to the underlying watcher Stop implementation.
func (w *WatcherWrapper) Stop() error {
	if w == nil || w.stop == nil {
		return nil
	}
	return w.stop()
}

// SetConfig updates the watcher configuration cache.
func (w *WatcherWrapper) SetConfig(cfg *config.Config) {
	if w == nil || w.setConfig == nil {
		return
	}
	w.setConfig(cfg)
}

// ReloadConfigIfChanged asks the underlying watcher to reload config from disk.
func (w *WatcherWrapper) ReloadConfigIfChanged() bool {
	if w == nil || w.reloadConfigIfChanged == nil {
		return false
	}
	w.reloadConfigIfChanged()
	return true
}

// SetPluginAuthParser updates the plugin auth parser used by the watcher.
func (w *WatcherWrapper) SetPluginAuthParser(parser PluginAuthParser) {
	if w == nil || w.setPluginAuthParser == nil {
		return
	}
	w.setPluginAuthParser(parser)
}

// DispatchRuntimeAuthUpdate forwards runtime auth updates (e.g., websocket providers)
// into the watcher-managed auth update queue when available.
// Returns true if the update was enqueued successfully.
func (w *WatcherWrapper) DispatchRuntimeAuthUpdate(update watcher.AuthUpdate) bool {
	if w == nil || w.dispatchRuntimeUpdate == nil {
		return false
	}
	return w.dispatchRuntimeUpdate(update)
}

// DispatchPersistedAuthUpdate forwards already-persisted file auth updates.
func (w *WatcherWrapper) DispatchPersistedAuthUpdate(update watcher.AuthUpdate) bool {
	if w == nil || w.dispatchPersistedAuth == nil {
		return false
	}
	return w.dispatchPersistedAuth(update)
}

// SetClients updates the watcher file-backed clients registry.
// SetClients and SetAPIKeyClients removed; watcher manages its own caches

// SnapshotClients returns the current combined clients snapshot from the underlying watcher.
// SnapshotClients removed; use SnapshotAuths

// SnapshotAuths returns the current auth entries derived from legacy clients.
func (w *WatcherWrapper) SnapshotAuths() []*coreauth.Auth {
	if w == nil || w.snapshotAuths == nil {
		return nil
	}
	return w.snapshotAuths()
}

// SetAuthUpdateQueue registers the channel used to propagate auth updates.
func (w *WatcherWrapper) SetAuthUpdateQueue(queue chan<- watcher.AuthUpdate) {
	if w == nil || w.setUpdateQueue == nil {
		return
	}
	w.setUpdateQueue(queue)
}
