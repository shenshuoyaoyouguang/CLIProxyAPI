// Package record defines the usage record types used across packages.
package record

import (
	"context"
	"sync"
	"time"
)

// Record contains the usage statistics captured for a single provider request.
type Record struct {
	Provider    string
	Model       string
	APIKey      string
	AuthID      string
	AuthIndex   string
	AuthType    string
	Source      string
	RequestedAt time.Time
	Latency     time.Duration
	Failed      bool
	Detail      Detail
}

// Detail holds the token usage breakdown.
type Detail struct {
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	CachedTokens    int64
	TotalTokens     int64
}

// Plugin consumes usage records emitted by the proxy runtime.
type Plugin interface {
	HandleUsage(ctx context.Context, record Record)
}

var (
	defaultPluginsMu sync.RWMutex
	defaultPlugins   []Plugin
)

// RegisterPlugin appends a plugin to the global plugin list.
func RegisterPlugin(p Plugin) {
	if p == nil {
		return
	}
	defaultPluginsMu.Lock()
	defaultPlugins = append(defaultPlugins, p)
	defaultPluginsMu.Unlock()
}

// Dispatch invokes all registered plugins with the given record.
func Dispatch(ctx context.Context, r Record) {
	defaultPluginsMu.RLock()
	plugins := defaultPlugins
	defaultPluginsMu.RUnlock()
	for _, p := range plugins {
		if p != nil {
			p.HandleUsage(ctx, r)
		}
	}
}
