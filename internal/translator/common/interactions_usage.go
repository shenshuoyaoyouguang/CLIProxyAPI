package common

import "github.com/tidwall/gjson"

func InteractionsUsage(root gjson.Result) gjson.Result {
	for _, path := range []string{
		"interaction.usage",
		"usage",
		"metadata.total_usage",
		"metadata.usage",
		"interaction.metadata.total_usage",
		"interaction.metadata.usage",
	} {
		if value := root.Get(path); value.Exists() {
			return value
		}
	}
	return gjson.Result{}
}

// FirstUsageInt returns the value of the first existing path parsed as an
// integer, plus whether any path matched.
func FirstUsageInt(root gjson.Result, paths ...string) (int64, bool) {
	for _, path := range paths {
		if v := root.Get(path); v.Exists() {
			return v.Int(), true
		}
	}
	return 0, false
}
