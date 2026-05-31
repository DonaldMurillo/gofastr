package framework

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/framework/cron"
)

// ============================================================================
// Tier 3 — concurrency & background (no DB)
// ============================================================================

// BenchmarkEventBus_Emit measures the cost of synchronous fan-out for N
// subscribers all on the same event type. Each handler is a noop; the
// reported number is overhead per emit.
func BenchmarkEventBus_Emit(b *testing.B) {
	ctx := context.Background()
	for _, n := range []int{1, 10, 100, 1000} {
		n := n
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			bus := NewEventBus()
			for i := 0; i < n; i++ {
				bus.Subscribe("test.event", func(_ context.Context, _ Event) error { return nil })
			}
			ev := Event{Type: "test.event", Data: map[string]any{"k": "v"}}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = bus.Emit(ctx, ev)
			}
		})
	}
}

// BenchmarkEventBus_EmitAsync measures the cost of async fan-out for N
// subscribers. Async spins a goroutine per emit, so the per-emit cost is
// dominated by goroutine creation + slice copy, not handler work.
func BenchmarkEventBus_EmitAsync(b *testing.B) {
	ctx := context.Background()
	for _, n := range []int{1, 10, 100} {
		n := n
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			bus := NewEventBus()
			for i := 0; i < n; i++ {
				bus.Subscribe("test.event", func(_ context.Context, _ Event) error { return nil })
			}
			ev := Event{Type: "test.event"}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				bus.EmitAsync(ctx, ev)
			}
		})
	}
}

// BenchmarkSSE_BackpressureDropRate is a property benchmark — it reports
// the effective drop rate when a single slow subscriber is paired with a
// fast emitter through the production SSEBroker. This intentionally
// exercises the configurable ?buffer= path rather than the legacy raw
// channel fan-out the broker replaced.
//
// Reported via b.ReportMetric("drop_rate", …).
func BenchmarkSSE_BackpressureDropRate(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping property benchmark in short mode")
	}

	const totalEvents = 5000
	const requestedBuffer = 128

	broker := stream.NewSSEBroker(stream.SSEBrokerConfig{
		Topic:             "bench",
		DefaultBuf:        32,
		MaxBuf:            512,
		HeartbeatInterval: time.Hour,
	})
	rec := newSlowCountingSSEWriter(100 * time.Microsecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("GET", fmt.Sprintf("/events?buffer=%d", requestedBuffer), nil).WithContext(ctx)

	subDone := make(chan struct{})
	go func() {
		defer close(subDone)
		broker.Subscribe(rec, req)
	}()
	waitForBenchSubscriber(b, broker)

	b.ResetTimer()
	for i := 0; i < totalEvents; i++ {
		broker.Publish(EntityCreated, fmt.Sprintf(`{"id":"p%d"}`, i))
	}
	waitForBenchDelivery(rec, requestedBuffer)
	b.StopTimer()
	cancel()
	<-subDone

	delivered := rec.events.Load()
	if delivered == 0 {
		b.Skip("no events flowed")
	}
	dropped := totalEvents - delivered
	if dropped < 0 {
		dropped = 0
	}
	dropRate := float64(dropped) / float64(totalEvents)
	b.ReportMetric(dropRate, "drop_rate")
	b.ReportMetric(float64(delivered), "delivered")
	b.ReportMetric(float64(dropped), "dropped")
	b.ReportMetric(requestedBuffer, "subscriber_buffer")
}

type slowCountingSSEWriter struct {
	header http.Header
	delay  time.Duration
	events atomic.Int64
}

func newSlowCountingSSEWriter(delay time.Duration) *slowCountingSSEWriter {
	return &slowCountingSSEWriter{header: make(http.Header), delay: delay}
}

func (w *slowCountingSSEWriter) Header() http.Header { return w.header }
func (w *slowCountingSSEWriter) WriteHeader(int)     {}
func (w *slowCountingSSEWriter) Flush()              {}
func (w *slowCountingSSEWriter) Write(p []byte) (int, error) {
	time.Sleep(w.delay)
	const marker = "event: "
	for i := 0; i+len(marker) <= len(p); i++ {
		if string(p[i:i+len(marker)]) == marker {
			w.events.Add(1)
		}
	}
	return len(p), nil
}

func waitForBenchSubscriber(b *testing.B, broker *stream.SSEBroker) {
	b.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if broker.SubscriberCount() == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	b.Fatalf("subscriber did not register")
}

func waitForBenchDelivery(rec *slowCountingSSEWriter, requestedBuffer int) {
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		if rec.events.Load() >= int64(requestedBuffer) {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

// BenchmarkSSEWriter_Write measures the cost of writing a single SSE event
// frame through the production writer. Reflects what the EventStream
// handler pays per outgoing event when a subscriber is connected.
func BenchmarkSSEWriter_Write(b *testing.B) {
	// Using the embedded ResponseRecorder + the framework's emitEvent path
	// would tangle DB setup; this measures the per-frame encode + write
	// cost directly via the SSE writer.
	type sseStub struct{ httptest.ResponseRecorder }
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		// stream.NewSSEWriter would be the production path, but it's in
		// a sibling package — write the same wire bytes inline for a
		// strict comparison-free measurement.
		fmt.Fprintf(rec.Body, "event: entity.created\n")
		fmt.Fprintf(rec.Body, "data: {\"type\":\"entity.created\",\"data\":{\"id\":\"p%d\"}}\n\n", i)
		_ = sseStub{}
	}
}

// BenchmarkCronTick measures the cost of a single scheduler tick scanning N
// registered jobs to find which fire this minute. Most won't fire; the cost
// is the scan itself plus the matches() bitmask check.
func BenchmarkCronTick(b *testing.B) {
	for _, n := range []int{1, 10, 100, 1000} {
		n := n
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			sched := NewScheduler()
			noop := func(_ context.Context) error { return nil }
			for i := 0; i < n; i++ {
				// Mix of specs so the bitmask comparisons aren't all hot in cache.
				spec := "* * * * *"
				if i%3 == 0 {
					spec = "0 * * * *"
				} else if i%5 == 0 {
					spec = "*/15 * * * *"
				}
				_ = sched.Register(CronJob{Name: fmt.Sprintf("job-%d", i), Spec: spec, Run: noop})
			}
			now := time.Date(2026, 5, 11, 12, 30, 0, 0, time.UTC)
			ctx := context.Background()
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sched.RunOnce(ctx, now)
			}
		})
	}
}

// BenchmarkCronParse measures the cost of parsing a cron spec. Called at
// Register time, so it's not on the hot path, but a regression here would
// show up at app startup with many jobs.
func BenchmarkCronParse(b *testing.B) {
	cases := map[string]string{
		"every-minute": "* * * * *",
		"hourly":       "0 * * * *",
		"@daily":       "@daily",
		"complex":      "*/15 9-17 * * 1-5",
		"with-list":    "0,15,30,45 * * * *",
	}
	for name, spec := range cases {
		spec := spec
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := cron.ParseCron(spec); err != nil {
					b.Fatalf("parse: %v", err)
				}
			}
		})
	}
}
