// Package gateway is a NOVEL instantiation of the control-bytes shape:
// an API-gateway edge filter (no such code exists in this repo) logging
// and exporting request-derived values. Same shape, different names
// and layout than any repo site.
package gateway

import (
	"log/slog"
	"net/http"

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

// sanitizeHeader matches the scrub-name clearance.
func sanitizeHeader(s string) string { return s }
