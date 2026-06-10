// Package tracing emits W3C trace-context-shaped span trees per turn.
//
// Span trees are written as JSON files at
// ~/.local/state/gofastr/harness/traces/<SessionID>/<TraceID>.json
// so the user can pipe them into a tool of their choice (jq, Jaeger
// import, etc.) without the harness pulling in a tracing library.
//
// Span hierarchy mirrors the agent loop:
//
//	turn (root span)
//	├── request-middleware-chain
//	├── provider.chat
//	│   └── stream-collect
//	├── tool-call (one span per dispatch)
//	│   ├── permission-gate
//	│   ├── sandbox-wrap
//	│   └── tool.run
//	└── persist-events
package tracing

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// TraceID and SpanID are W3C trace-context-compliant: TraceID is 16
// bytes (32 hex chars), SpanID is 8 bytes (16 hex chars).
type TraceID [16]byte
type SpanID [8]byte

func (t TraceID) String() string { return hex.EncodeToString(t[:]) }
func (s SpanID) String() string  { return hex.EncodeToString(s[:]) }

// NewTraceID returns a fresh random TraceID.
func NewTraceID() TraceID {
	var t TraceID
	_, _ = rand.Read(t[:])
	return t
}

// NewSpanID returns a fresh random SpanID.
func NewSpanID() SpanID {
	var s SpanID
	_, _ = rand.Read(s[:])
	return s
}

// Span is one node in the trace tree.
type Span struct {
	TraceID    string         `json:"trace_id"`
	SpanID     string         `json:"span_id"`
	ParentID   string         `json:"parent_id,omitempty"`
	Name       string         `json:"name"`
	StartedAt  time.Time      `json:"started_at"`
	EndedAt    time.Time      `json:"ended_at"`
	DurationNS int64          `json:"duration_ns"`
	Status     string         `json:"status,omitempty"` // "ok" | "error"
	Attributes map[string]any `json:"attributes,omitempty"`
}

// Trace is one full per-turn span tree.
type Trace struct {
	TraceID   string    `json:"trace_id"`
	SessionID string    `json:"session_id"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	Spans     []Span    `json:"spans"`
}

// Recorder collects spans for one turn and writes the trace to disk
// when Done is called.
type Recorder struct {
	dir       string
	session   ids.SessionID
	traceID   TraceID
	mu        sync.Mutex
	spans     []Span
	rootSpan  SpanID
	startedAt time.Time
	active    map[SpanID]int // spanID → index in spans (for End)
}

// NewRecorder constructs a Recorder rooted at `dir`. The actual file
// is written to <dir>/<SessionID>/<TraceID>.json on Done.
func NewRecorder(dir string, session ids.SessionID) *Recorder {
	return &Recorder{
		dir:       dir,
		session:   session,
		traceID:   NewTraceID(),
		active:    make(map[SpanID]int),
		startedAt: time.Now(),
	}
}

// TraceID returns the recorder's trace ID.
func (r *Recorder) TraceID() TraceID { return r.traceID }

// Start opens a span. Returns the span's ID; pass it to End. If
// parent is the zero SpanID, the new span is rooted at the trace.
func (r *Recorder) Start(parent SpanID, name string, attrs map[string]any) SpanID {
	id := NewSpanID()
	r.mu.Lock()
	defer r.mu.Unlock()
	parentStr := ""
	if parent != (SpanID{}) {
		parentStr = parent.String()
	} else if r.rootSpan == (SpanID{}) {
		r.rootSpan = id
	}
	idx := len(r.spans)
	r.spans = append(r.spans, Span{
		TraceID:    r.traceID.String(),
		SpanID:     id.String(),
		ParentID:   parentStr,
		Name:       name,
		StartedAt:  time.Now(),
		Attributes: attrs,
	})
	r.active[id] = idx
	return id
}

// End closes a previously-started span.
func (r *Recorder) End(id SpanID, status string, attrs map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	idx, ok := r.active[id]
	if !ok {
		return
	}
	delete(r.active, id)
	s := &r.spans[idx]
	s.EndedAt = time.Now()
	s.DurationNS = s.EndedAt.Sub(s.StartedAt).Nanoseconds()
	if status != "" {
		s.Status = status
	}
	for k, v := range attrs {
		if s.Attributes == nil {
			s.Attributes = make(map[string]any)
		}
		s.Attributes[k] = v
	}
}

// Done writes the trace tree to disk.
func (r *Recorder) Done() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	dir := filepath.Join(r.dir, string(r.session))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, r.traceID.String()+".json")
	tr := Trace{
		TraceID:   r.traceID.String(),
		SessionID: string(r.session),
		StartedAt: r.startedAt,
		EndedAt:   time.Now(),
		Spans:     r.spans,
	}
	data, err := json.MarshalIndent(tr, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
}

// TraceContextHeader returns the W3C `traceparent` header value for
// the given span: `00-<TraceID>-<SpanID>-01` (sampled).
//
// Useful when the harness calls out to external services that
// understand W3C trace context (so a Copilot or OpenRouter
// request can be correlated back to the harness turn).
func TraceContextHeader(traceID TraceID, spanID SpanID) string {
	return fmt.Sprintf("00-%s-%s-01", traceID.String(), spanID.String())
}

// ErrNoRecorder is returned when API calls expect a recorder bound
// to a context but none is present.
var ErrNoRecorder = errors.New("tracing: no recorder bound")
