package common

import (
	"strconv"
	"strings"
)

// toolNameLimit is the maximum length allowed for tool names in requests that
// reject long names.
const toolNameLimit = 64

// ShortenToolName shortens tool names longer than the 64-character limit,
// preserving the "mcp__" prefix and last "__" segment when present.
func ShortenToolName(name string) string {
	if len(name) <= toolNameLimit {
		return name
	}
	if strings.HasPrefix(name, "mcp__") {
		idx := strings.LastIndex(name, "__")
		if idx > 0 {
			candidate := "mcp__" + name[idx+2:]
			if len(candidate) > toolNameLimit {
				return candidate[:toolNameLimit]
			}
			return candidate
		}
	}
	return name[:toolNameLimit]
}

// BuildShortNameMap ensures uniqueness of shortened names within a request.
// It maps each original name to a shortened form, appending numeric suffixes
// when two names would otherwise collide.
func BuildShortNameMap(names []string) map[string]string {
	used := map[string]struct{}{}
	m := map[string]string{}

	baseCandidate := func(n string) string {
		if len(n) <= toolNameLimit {
			return n
		}
		if strings.HasPrefix(n, "mcp__") {
			idx := strings.LastIndex(n, "__")
			if idx > 0 {
				cand := "mcp__" + n[idx+2:]
				if len(cand) > toolNameLimit {
					cand = cand[:toolNameLimit]
				}
				return cand
			}
		}
		return n[:toolNameLimit]
	}

	makeUnique := func(cand string) string {
		if _, ok := used[cand]; !ok {
			return cand
		}
		base := cand
		for i := 1; ; i++ {
			suffix := "_" + strconv.Itoa(i)
			allowed := toolNameLimit - len(suffix)
			if allowed < 0 {
				allowed = 0
			}
			tmp := base
			if len(tmp) > allowed {
				tmp = tmp[:allowed]
			}
			tmp = tmp + suffix
			if _, ok := used[tmp]; !ok {
				return tmp
			}
		}
	}

	for _, n := range names {
		cand := baseCandidate(n)
		uniq := makeUnique(cand)
		used[uniq] = struct{}{}
		m[n] = uniq
	}
	return m
}
