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
}

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
	failed int // spans that failed, the root included
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
	// Make room: first the traces whose root never came, then the
	// oldest until the buffer fits. The map is bounded by the same cap,
	// since every entry holds at least one span's cost.
	for front := c.order.Front(); front != nil; front = c.order.Front() {
		old := front.Value.(*openTrace)
		if c.bytes <= c.cfg.MaxMemoryBytes && now.Sub(old.first) < maxOpenAge {
			break
		}
		c.remove(old)
		c.dropped.Add(1)
	}
	c.mu.Unlock()

	if !e.Root {
		return
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
	t := Trace{
		Time: e.Start.Add(s.Dur), TraceID: e.TraceID, Route: e.Route, Path: e.Path, Status: e.Status,
		Dur: s.Dur, Error: e.Error, FailedSteps: ot.failed, Spans: ot.spans,
	}
	// The sink runs outside the lock: a slow disk must not stall every
	// span ending in the process.
	if err := c.sink(t); err != nil {
		c.writeErrs.Add(1)
		return
	}
	c.kept.Add(1)
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
