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

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// TraceID and SpanID are W3C trace-context IDs.
type (
	TraceID [16]byte
	SpanID  [8]byte
)

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

// Span is one node of a trace tree; Status is "ok" or "error".
type Span struct {
	TraceID    string         `json:"trace_id"`
	SpanID     string         `json:"span_id"`
	ParentID   string         `json:"parent_id,omitempty"`
	Name       string         `json:"name"`
	StartedAt  time.Time      `json:"started_at"`
	EndedAt    time.Time      `json:"ended_at"`
	DurationNS int64          `json:"duration_ns"`
	Status     string         `json:"status,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// Trace is one turn's span tree as written to disk.
type Trace struct {
	TraceID   string    `json:"trace_id"`
	SessionID string    `json:"session_id"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	Spans     []Span    `json:"spans"`
}

// Recorder collects one turn's spans and writes them with Done.
type Recorder struct {
	dir       string
	session   ids.SessionID
	traceID   TraceID
	mu        sync.Mutex
	spans     []Span
	rootSpan  SpanID
	startedAt time.Time
	active    map[SpanID]int
}

// NewRecorder returns a Recorder whose Done writes <dir>/<session>/<TraceID>.json.
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

// Start opens a span under parent, or at the root for the zero SpanID, and returns its ID for End.
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

// End closes an open span, setting status when non-empty and merging attrs; an unknown ID is ignored.
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

// Done writes the trace file owner-only and returns its path.
func (r *Recorder) Done() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	dir := filepath.Join(r.dir, string(r.session))
	if err := os.MkdirAll(dir, 0o700); err != nil {
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
	if err := fileperm.WriteOwnerOnly(path, data); err != nil {
		return "", err
	}
	return path, nil
}

// TraceContextHeader returns the W3C traceparent value "00-<trace>-<span>-01" (sampled).
func TraceContextHeader(traceID TraceID, spanID SpanID) string {
	return fmt.Sprintf("00-%s-%s-01", traceID.String(), spanID.String())
}

// ErrNoRecorder reports that no Recorder is bound where one is expected.
var ErrNoRecorder = errors.New("tracing: no recorder bound")
