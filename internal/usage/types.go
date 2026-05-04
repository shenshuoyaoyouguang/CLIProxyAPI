package usage

import "github.com/router-for-me/CLIProxyAPI/v6/internal/usage/types"

type TokenStats = types.TokenStats
type UsageDetail = types.UsageDetail
type ModelEntry = types.ModelEntry
type APIEntry = types.APIEntry
type UsageSnapshot = types.UsageSnapshot
type QueuedUsageDetail = types.QueuedUsageDetail
type ExportPayload = types.ExportPayload
type ImportResponse = types.ImportResponse

const (
	DefaultMaxEvents     = types.DefaultMaxEvents
	ExportMaxEvents     = types.ExportMaxEvents
	UsageEventsLimit    = types.UsageEventsLimit
	UsageEventsMaxLimit = types.UsageEventsMaxLimit
)
