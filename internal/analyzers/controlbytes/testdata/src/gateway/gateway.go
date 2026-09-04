// Package gateway is a NOVEL instantiation of the control-bytes shape:
// an API-gateway edge filter (no such code exists in this repo) logging
// and exporting request-derived values. Same shape, different names
// and layout than any repo site.
package gateway

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"attribute"
)

// EdgeFilter is invented: per-route telemetry at the edge.
type EdgeFilter struct {
	log *slog.Logger
}

func (e *EdgeFilter) observe(r *http.Request) {
	ua := r.Header.Get("User-Agent")
	traceparent := r.Header.Get("Traceparent")
	e.log.Warn("gateway: rejected at edge", // want `controlbytes: request-derived value reaches logger.Debug/Info/Warn/Error key-value unscrubbed`
		"user_agent", ua,
		"traceparent", traceparent,
		"route", r.PathValue("route"),
	)
	_ = attribute.String("edge.host", r.Host) // want `controlbytes: request-derived value reaches attribute.String unscrubbed`
}

func (e *EdgeFilter) observeScrubbed(r *http.Request) {
	e.log.Warn("gateway: rejected at edge",
		"user_agent", sanitizeHeader(r.Header.Get("User-Agent")),
		"traceparent", sanitizeHeader(r.Header.Get("Traceparent")),
		"route", sanitizeHeader(r.PathValue("route")),
	)
	_ = attribute.String("edge.host", sanitizeHeader(r.Host))
}

// sanitizeHeader percent-encodes C0/DEL: the name says scrub and the
// body shows the byte walk that earns it.
func sanitizeHeader(s string) string {
	for i := range s {
		if c := s[i]; c < 0x20 || c == 0x7f {
			var b strings.Builder
			for j := range s {
				if d := s[j]; d < 0x20 || d == 0x7f {
					fmt.Fprintf(&b, "%%%02x", s[j])
					continue
				}
				b.WriteByte(s[j])
			}
			return b.String()
		}
	}
	return s
}
