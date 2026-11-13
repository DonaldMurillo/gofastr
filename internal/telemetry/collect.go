package telemetry

import (
	"container/list"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Limits on one trace, so a line stays far below the readers' cap and a
// runaway loop of spans cannot fill the buffer on its own.
const (
	maxSpansPerTrace = 512
	maxAttrsPerSpan  = 32
	maxAttrValue     = 1024
	maxSpanName      = 256
	// maxOpenAge is how long a trace waits for its root span to end
	// before its spans are given up.
	maxOpenAge = 10 * time.Minute
)

// SpanEnd is one finished span handed to a Collector.
type SpanEnd struct {
	TraceID string // 32 hex digits
	Span    Span   // Offset is ignored; Start places the span
	Start   time.Time
	// Root marks the span whose end completes the trace in this
	// process: one with no parent, or whose parent is in another
	// process.
	Root bool
	// Error marks a span that failed. A failed root keeps the trace; a
	// failed step under it is recorded and counted, and decides nothing:
	// a handler that deals with a query's error has not failed.
	Error bool
	// Err is what a failed span says went wrong; empty for one that
	// did not fail.
	Err string
	// Route, Path and Status describe the trace; they are read from the
	// root.
	Route  string
	Path   string
	Status int
	// Stray marks a root that is one outgoing call with nothing around
	// it: a query a background loop ran outside any request or job. It
	// is kept only when it was slow. A poll every second would fill the
	// list as a sample, and a failure here is often a probe that is
	// meant to fail (asking SQLite for version() to learn it is not
	// Postgres); a real one is the caller's to log.
	Stray bool
	// Quiet marks a request nobody opens the list to see: a fetch of
	// the framework's own scripts and styles. It is kept when it failed
	// or was slow, and never as part of the sample.
	Quiet bool
}

// Collector buffers finished spans by trace and, when a trace's root
// ends, decides whether to keep it: always when the root failed or the
// trace was slow, otherwise for SampleRate of them. Because the decision waits
// for the whole trace, nothing interesting is sampled away. The buffer
// never exceeds MaxMemoryBytes: past it, the oldest waiting trace is
// dropped and counted.
type Collector struct {
	cfg  Config
	sink func(Trace) error
	now  func() time.Time

	mu    sync.Mutex
	open  map[string]*openTrace
	order *list.List // of *openTrace, oldest first
	bytes int64

	kept, sampledOut, dropped, spansDropped, writeErrs atomic.Int64

	// alerts, when set, counts every finished request, kept or not: a
	// rule about failures must see them all. Set before the first span.
	alerts *Alerts
}

// WatchAlerts has the collector count every finished request against a.
// Call it before the collector sees a span.
func (c *Collector) WatchAlerts(a *Alerts) { c.alerts = a }

// CollectorStats counts what a Collector has decided.
type CollectorStats struct {
	// Kept and SampledOut are finished traces written and not written.
	Kept, SampledOut int64
	// Dropped is the traces given up unfinished: the buffer was full or
	// the root never ended.
	Dropped int64
	// SpansDropped is the spans past a trace's span limit.
	SpansDropped int64
	// WriteErrors is the kept traces the sink refused.
	WriteErrors int64
	// OpenTraces and BufferedBytes describe the buffer now.
	OpenTraces    int
	BufferedBytes int64
}

type openTrace struct {
	id     string
	first  time.Time
	spans  []Span
	starts []time.Time
	bytes  int64
	failed int    // spans that failed, the root included
	err    string // the first failed step's Err
	logErr string // the first error the app logged for the trace
	elem   *list.Element
}

// NewCollector returns a collector that hands kept traces to sink,
// usually a Writer's WriteTrace.
func NewCollector(cfg Config, sink func(Trace) error) (*Collector, error) {
	cfg, err := cfg.Resolve()
	if err != nil {
		return nil, err
	}
	return &Collector{cfg: cfg, sink: sink, now: time.Now, open: map[string]*openTrace{}, order: list.New()}, nil
}

// Stats returns the counters and the buffer's current size.
func (c *Collector) Stats() CollectorStats {
	c.mu.Lock()
	open, buffered := len(c.open), c.bytes
	c.mu.Unlock()
	return CollectorStats{
		Kept: c.kept.Load(), SampledOut: c.sampledOut.Load(), Dropped: c.dropped.Load(),
		SpansDropped: c.spansDropped.Load(), WriteErrors: c.writeErrs.Load(),
		OpenTraces: open, BufferedBytes: buffered,
	}
}

func cut(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// spanCost is what a span counts for against MaxMemoryBytes: its
// strings plus the fixed part of the structs holding it.
func spanCost(s Span) int64 {
	n := 160 + len(s.SpanID) + len(s.ParentID) + len(s.Name)
	for _, a := range s.Attrs {
		n += 32 + len(a.Key) + len(a.Value)
	}
	return int64(n)
}

// SpanEnded records one finished span.
func (c *Collector) SpanEnded(e SpanEnd) {
	s := e.Span
	s.Error = e.Error
	s.Name = cut(s.Name, maxSpanName)
	if len(s.Attrs) > maxAttrsPerSpan {
		s.Attrs = s.Attrs[:maxAttrsPerSpan]
	}
	for i := range s.Attrs {
		s.Attrs[i].Value = cut(s.Attrs[i].Value, maxAttrValue)
	}
	cost := spanCost(s)
	now := c.now()

	c.mu.Lock()
	ot := c.open[e.TraceID]
	if ot == nil {
		ot = &openTrace{id: e.TraceID, first: now}
		ot.elem = c.order.PushBack(ot)
		c.open[e.TraceID] = ot
	}
	if e.Error {
		ot.failed++
		// The root is the whole, and says so in the status's words
		// ("Internal Server Error"); a step says what broke.
		if !e.Root && ot.err == "" {
			ot.err = e.Err
		}
	}
	if len(ot.spans) < maxSpansPerTrace || e.Root {
		ot.spans = append(ot.spans, s)
		ot.starts = append(ot.starts, e.Start)
		ot.bytes += cost
		c.bytes += cost
	} else {
		c.spansDropped.Add(1)
	}
	if e.Root {
		c.remove(ot)
	}
	c.makeRoom(now)
	c.mu.Unlock()

	if !e.Root {
		return
	}
	// A lone outgoing call is not a request, and is not counted as one.
	if c.alerts != nil && !e.Stray {
		c.alerts.Observe(e.Start.Add(s.Dur), e.Route, e.Error, s.Dur >= c.cfg.SlowThreshold)
	}
	keep := s.Dur >= c.cfg.SlowThreshold
	if !e.Stray {
		keep = keep || e.Error || (!e.Quiet && sampled(e.TraceID, c.cfg.SampleRate))
	}
	if !keep {
		c.sampledOut.Add(1)
		return
	}
	// The root goes first; every span is placed from the root's start.
	last := len(ot.spans) - 1
	ot.spans[0], ot.spans[last] = ot.spans[last], ot.spans[0]
	ot.starts[0], ot.starts[last] = ot.starts[last], ot.starts[0]
	for i := range ot.spans {
		ot.spans[i].Offset = ot.starts[i].Sub(e.Start)
	}
	if e.Error {
		ot.failed-- // the root is the whole, not a step
	}
	// What went wrong, from whoever says it best: the first step that
	// failed, else the first error the app logged while handling the
	// request, else the whole's own status text (which for a panic is
	// the panic).
	errText := ot.err
	if errText == "" && (e.Error || ot.failed > 0) {
		errText = ot.logErr
	}
	if errText == "" && e.Error {
		errText = e.Err
	}
	t := Trace{
		Time: e.Start.Add(s.Dur), TraceID: e.TraceID, Route: e.Route, Path: e.Path, Status: e.Status,
		Dur: s.Dur, Error: e.Error, FailedSteps: ot.failed, Err: errText, Spans: ot.spans,
	}
	// The sink runs outside the lock: a slow disk must not stall every
	// span ending in the process.
	if err := c.sink(t); err != nil {
		c.writeErrs.Add(1)
		return
	}
	c.kept.Add(1)
}

// NoteError records an error the app logged inside a trace, for a
// request that fails without a failed step: the handler that logs what
// went wrong and answers 500. The first one for a trace is kept, and a
// trace that never fails makes nothing of it. The line usually comes
// before any span of the request has ended, so it opens the trace's
// place in the buffer, which the same cap and the same waiting limit
// hold.
func (c *Collector) NoteError(traceID, text string) {
	if text == "" {
		return
	}
	text = cut(text, maxErrLen)
	c.mu.Lock()
	defer c.mu.Unlock()
	ot := c.open[traceID]
	if ot == nil {
		ot = &openTrace{id: traceID, first: c.now()}
		ot.elem = c.order.PushBack(ot)
		c.open[traceID] = ot
	}
	if ot.logErr == "" {
		ot.logErr = text
		cost := int64(160 + len(text))
		ot.bytes += cost
		c.bytes += cost
		c.makeRoom(ot.first)
	}
}

// LoggedError reads a log entry for an error worth naming a failure by:
// a line at ERROR or above inside a trace, and its "err" (or "error")
// attribute, or its message when it has neither.
func LoggedError(entry []byte) (traceID, text string, ok bool) {
	lv, found := stringField(entry, `"level":`)
	if !found || len(lv) < 5 || lv[:5] != "ERROR" {
		return "", "", false
	}
	if traceID, found = stringField(entry, `"trace_id":`); !found || len(traceID) != 32 {
		return "", "", false
	}
	for _, key := range []string{`"err":`, `"error":`, `"msg":`} {
		if text, found = stringField(entry, key); found && text != "" {
			return traceID, text, true
		}
	}
	return "", "", false
}

// makeRoom drops waiting traces until the buffer fits: first the ones
// whose root never came, then the oldest. The map is bounded by the
// same cap, since every entry holds at least one span's or one logged
// error's cost. c.mu must be held.
func (c *Collector) makeRoom(now time.Time) {
	for front := c.order.Front(); front != nil; front = c.order.Front() {
		old := front.Value.(*openTrace)
		if c.bytes <= c.cfg.MaxMemoryBytes && now.Sub(old.first) < maxOpenAge {
			break
		}
		c.remove(old)
		c.dropped.Add(1)
	}
}

// remove takes ot out of the buffer. c.mu must be held.
func (c *Collector) remove(ot *openTrace) {
	c.order.Remove(ot.elem)
	delete(c.open, ot.id)
	c.bytes -= ot.bytes
}

// sampled reports whether traceID falls in the kept share. It reads the
// ID, not a random number, so every process that sees the trace decides
// the same way.
func sampled(traceID string, rate float64) bool {
	if len(traceID) < 16 {
		return false
	}
	v, err := strconv.ParseUint(traceID[:16], 16, 64)
	if err != nil {
		return false
	}
	// A share in [0, 1): below a rate of 1 always, below 0 never.
	return float64(v>>11)/(1<<53) < rate
}
