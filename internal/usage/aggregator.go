package usage

import (
	"encoding/json"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/redisqueue"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage/types"
	"github.com/sirupsen/logrus"
)

type Aggregator struct {
	maxEvents int
}

func NewAggregator(maxEvents int) *Aggregator {
	if maxEvents <= 0 {
		maxEvents = 1000
	}
	return &Aggregator{maxEvents: maxEvents}
}

func (a *Aggregator) GetSnapshot() *types.UsageSnapshot {
	rawEvents := redisqueue.PeekOldest(a.maxEvents)
	if len(rawEvents) == 0 {
		return &types.UsageSnapshot{APIs: make(map[string]types.APIEntry)}
	}

	apis := make(map[string]types.APIEntry)

	for i, raw := range rawEvents {
		if len(raw) == 0 {
			continue
		}

		var detail types.QueuedUsageDetail
		if err := json.Unmarshal(raw, &detail); err != nil {
			logrus.WithError(err).WithField("index", i).WithField("raw_len", len(raw)).Debug("failed to unmarshal usage event")
			continue
		}

		endpoint := strings.TrimSpace(detail.Endpoint)
		if endpoint == "" {
			endpoint = "unknown"
		}

		model := strings.TrimSpace(detail.Model)
		if model == "" {
			model = "unknown"
		}

		apiEntry, apiExists := apis[endpoint]
		if !apiExists {
			apiEntry = types.APIEntry{
				Models: make(map[string]types.ModelEntry),
			}
		}

		modelEntry, modelExists := apiEntry.Models[model]
		if !modelExists {
			modelEntry = types.ModelEntry{
				Details: make([]types.UsageDetail, 0),
			}
		}

		apiEntry.TotalRequests++
		modelEntry.TotalRequests++

		if detail.Failed {
			apiEntry.FailureCount++
			modelEntry.FailureCount++
		} else {
			apiEntry.SuccessCount++
			modelEntry.SuccessCount++
		}

		totalTokens := detail.Tokens.TotalTokens
		apiEntry.TotalTokens += totalTokens
		modelEntry.TotalTokens += totalTokens

		modelEntry.Details = append(modelEntry.Details, detail.ToUsageDetail())

		apiEntry.Models[model] = modelEntry
		apis[endpoint] = apiEntry
	}

	return &types.UsageSnapshot{APIs: apis}
}
