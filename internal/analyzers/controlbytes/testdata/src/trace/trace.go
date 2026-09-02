// Package trace is a minimal stand-in for
// go.opentelemetry.io/otel/trace (see the attribute stub for why the
// fixtures carry local copies). Only the span-start/rename shape the
// controlbytes fixtures exercise is modeled.
package trace

import "context"

type Span struct{ name string }

func (s *Span) End()             {}
func (s *Span) SetName(n string) { s.name = n }

type Tracer interface {
	Start(ctx context.Context, name string, opts ...SpanOption) (context.Context, *Span)
}

type SpanOption struct{ Attrs []any }

func WithAttributes(attrs ...any) SpanOption { return SpanOption{Attrs: attrs} }
