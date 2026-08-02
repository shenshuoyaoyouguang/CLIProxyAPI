package management

import (
	"github.com/router-for-me/CLIProxyAPI/v7/internal/ratelimit"
)

func totalActive(stats []ratelimit.ShardStats) int {
	sum := 0
	for _, s := range stats {
		sum += s.Active
	}
	return sum
}

func totalWaiting(stats []ratelimit.ShardStats) int {
	sum := 0
	for _, s := range stats {
		sum += s.Waiting
	}
	return sum
}

func total429(stats []ratelimit.ShardStats) int64 {
	sum := int64(0)
	for _, s := range stats {
		sum += s.Total429
	}
	return sum
}
