package helps

import (
	"net/http"
	"strings"
)

// HeaderValueCaseInsensitive returns the first non-empty header value matching
// name case-insensitively. The canonical lookup is tried first, then a
// case-insensitive key scan so manually built maps with non-canonical or
// whitespace-padded keys still resolve. Values are returned as-is: trimming is
// a caller decision, so strict equality gates (e.g. Claude Code client
// detection) compare against the exact wire value.
func HeaderValueCaseInsensitive(headers http.Header, name string) string {
	name = strings.TrimSpace(name)
	if headers == nil || name == "" {
		return ""
	}
	if val := headers.Get(name); val != "" {
		return val
	}
	for key, values := range headers {
		if !strings.EqualFold(key, name) {
			continue
		}
		for _, value := range values {
			if value != "" {
				return value
			}
		}
	}
	return ""
}
