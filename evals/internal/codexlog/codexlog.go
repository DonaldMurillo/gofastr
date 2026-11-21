// Package codexlog parses the telemetry lines Codex CLI sessions leave
// in their logs.
package codexlog

import (
	"regexp"
	"strconv"
	"strings"
)

var tokenUsagePattern = regexp.MustCompile(`(?m)tokens used\r?\n([\d,]+)`)

// ParseTokenUsage reports the token count from the last "tokens used"
// summary line in a Codex session log, or 0 when there is none.
func ParseTokenUsage(log []byte) int64 {
	matches := tokenUsagePattern.FindAllSubmatch(log, -1)
	if len(matches) == 0 {
		return 0
	}
	raw := strings.ReplaceAll(string(matches[len(matches)-1][1]), ",", "")
	value, _ := strconv.ParseInt(raw, 10, 64)
	return value
}
