// Package otelsink mirrors core/middleware's Tracing middleware: span
// attributes carrying request values, with the scrubbed spellings as
// negatives. Span NAMES are deliberately not sinks (only
// attribute.String values are), which the goodName case pins.
package otelsink

import (
	"net/http"

	"attribute"
	"trace"
)

func scrubCtl(s string) string {
	out := make([]byte, 0, len(s))
	for i := range s {
		if c := s[i]; c >= 0x20 && c != 0x7f {
			out = append(out, c)
		}
	}
	return string(out)
}

// badSpan is the pre-fix Tracing reduced to the shape.
func badSpan(tracer trace.Tracer, w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "HTTP "+r.Method, // span name: not a sink
		trace.WithAttributes(
			attribute.String("http.method", r.Method),   // want `controlbytes: request-derived value reaches attribute.String unscrubbed`
			attribute.String("http.target", r.URL.Path), // want `controlbytes: request-derived value reaches attribute.String unscrubbed`
		),
	)
	defer span.End()
	span.SetName("HTTP " + r.Method + " " + r.URL.Path) // SetName: not a sink
	_ = ctx
}

// goodSpan is the fixed spelling.
func goodSpan(tracer trace.Tracer, w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "HTTP "+scrubCtl(r.Method),
		trace.WithAttributes(
			attribute.String("http.method", scrubCtl(r.Method)),
			attribute.String("http.target", scrubCtl(r.URL.Path)),
		),
	)
	defer span.End()
	span.SetName("HTTP " + r.URL.Path)
	_ = ctx
}
