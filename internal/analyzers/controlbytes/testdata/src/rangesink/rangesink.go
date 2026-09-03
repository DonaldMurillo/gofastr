// Package rangesink pins the range-value binding (review finding C3):
// a `for _, seg := range segs` value variable is exactly as
// request-derived as segs[i] — only the spelling differs — so both must
// reach the sink, and a scrub over the range value must still clear.
package rangesink

import (
	"log/slog"
	"net/http"
	"strings"
)

func xff(r *http.Request) {
	segs := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := range segs {
		_ = slog.String("xff_index", segs[i]) // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
	}
	for _, seg := range segs {
		_ = slog.String("xff_range", seg) // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
	}
}

func rangeScrubbed(r *http.Request) {
	segs := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for _, seg := range segs {
		_ = slog.String("xff_range", scrubCtl(seg))
	}
}

// mapRange: the value variable of a range over a request-derived map is
// bound the same way.
func mapRange(r *http.Request) {
	copied := map[string]string{"xff": r.Header.Get("X-Forwarded-For")}
	for _, v := range copied {
		_ = slog.String("copied", v) // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
	}
}

func scrubCtl(s string) string {
	if !strings.ContainsAny(s, "\x00\x01\x1b\x7f\r\n") {
		return s
	}
	var b strings.Builder
	for i := range s {
		if c := s[i]; c >= 0x20 && c != 0x7f {
			b.WriteByte(c)
		}
	}
	return b.String()
}
