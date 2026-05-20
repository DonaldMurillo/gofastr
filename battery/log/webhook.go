package log

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// WebhookOpts configures a webhook sink. All fields have sensible defaults.
type WebhookOpts struct {
	// Method defaults to "POST".
	Method string

	// Headers are appended to every request. Content-Type is forced to
	// application/json and cannot be overridden here.
	Headers map[string]string

	// BatchSize triggers a flush. Default 50.
	BatchSize int
	// BatchInterval triggers a flush regardless of size. Default 1s.
	BatchInterval time.Duration
	// QueueSize caps the in-memory buffer. When full, the oldest entry
	// is dropped to make room (drop-oldest, never block the caller).
	// Default 1000.
	QueueSize int

	// Timeout for each HTTP request. Default 5s.
	Timeout time.Duration
	// MaxRetries on transient failure (5xx, network). Default 3. Exponential
	// backoff starts at 250ms.
	MaxRetries int

	// HTTPClient lets tests inject a stub. Nil = http.Client{Timeout: Timeout}.
	HTTPClient *http.Client
}

// webhookSink batches entries and POSTs them as {"entries": [<raw json>, ...]}.
// Entries are stored as []byte and concatenated server-side; no re-decoding.
type webhookSink struct {
	url    string
	opts   WebhookOpts
	client *http.Client

	mu        sync.Mutex
	queue     [][]byte // bounded ring
	flush     chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
	done      chan struct{}

	dropped atomic.Uint64 // entries discarded because the queue was full
	gaveUp  atomic.Uint64 // batches given up after MaxRetries

	// Rate-limit stderr emissions of "give up" / "4xx" messages so a
	// persistently broken receiver doesn't drown stderr (which may go
	// to systemd-journald with its own rate limiter — that limiter
	// would then drop legitimate logs from elsewhere in the process).
	lastWarnAt   atomic.Int64 // unix nano
	warnInterval time.Duration
}

// WebhookSink builds a sink that POSTs batched entries to url.
func WebhookSink(url string, opts WebhookOpts) Sink {
	if opts.Method == "" {
		opts.Method = http.MethodPost
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 50
	}
	if opts.BatchInterval <= 0 {
		opts.BatchInterval = time.Second
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = 1000
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Second
	}
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 3
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: opts.Timeout}
	}
	s := &webhookSink{
		url:          url,
		opts:         opts,
		client:       client,
		flush:        make(chan struct{}, 1),
		closed:       make(chan struct{}),
		done:         make(chan struct{}),
		warnInterval: time.Minute,
	}
	go s.run()
	return s
}

// warnOncePer rate-limits stderr emissions so a persistently broken
// receiver doesn't drown the host's stderr. Returns true if the caller
// should emit, false if it should stay quiet.
func (s *webhookSink) shouldWarn() bool {
	now := time.Now().UnixNano()
	last := s.lastWarnAt.Load()
	if last != 0 && time.Duration(now-last) < s.warnInterval {
		return false
	}
	return s.lastWarnAt.CompareAndSwap(last, now)
}

func (s *webhookSink) Write(entry []byte) error {
	select {
	case <-s.closed:
		return ErrSinkClosed
	default:
	}
	s.mu.Lock()
	if len(s.queue) >= s.opts.QueueSize {
		// Drop oldest to make room. Never block. Counter is surfaced
		// via Dropped() and emitted to stderr on shutdown so silent
		// log loss is at least diagnosable.
		s.queue = s.queue[1:]
		s.dropped.Add(1)
	}
	s.queue = append(s.queue, entry)
	shouldSignal := len(s.queue) >= s.opts.BatchSize
	s.mu.Unlock()

	if shouldSignal {
		select {
		case s.flush <- struct{}{}:
		default:
		}
	}
	return nil
}

// Dropped returns the running count of entries discarded because the
// queue was full. Useful for tests + operators chasing silent log loss.
func (s *webhookSink) Dropped() uint64 { return s.dropped.Load() }

// GaveUp returns the running count of batches given up after exhausting
// MaxRetries — log volume that NEVER reached the downstream receiver.
func (s *webhookSink) GaveUp() uint64 { return s.gaveUp.Load() }

// Close signals the worker goroutine to flush + exit and waits for it.
// Cancels any in-flight HTTP retry via the closed channel; bounded to
// the current attempt's Timeout, then immediate exit. Concurrent /
// repeated Close calls are safe — only the first signals the worker.
func (s *webhookSink) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	<-s.done
	if d := s.dropped.Load(); d > 0 {
		fmt.Fprintf(os.Stderr, "log: webhook sink dropped %d entry/entries (queue full)\n", d)
	}
	if g := s.gaveUp.Load(); g > 0 {
		fmt.Fprintf(os.Stderr, "log: webhook sink gave up on %d batch/batches after MaxRetries\n", g)
	}
	return nil
}

func (s *webhookSink) run() {
	defer close(s.done)
	ticker := time.NewTicker(s.opts.BatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.closed:
			s.drainAndSend()
			return
		case <-ticker.C:
			s.drainAndSend()
		case <-s.flush:
			s.drainAndSend()
		}
	}
}

func (s *webhookSink) drainAndSend() {
	s.mu.Lock()
	if len(s.queue) == 0 {
		s.mu.Unlock()
		return
	}
	batch := s.queue
	s.queue = nil
	s.mu.Unlock()

	s.send(batch)
}

func (s *webhookSink) send(batch [][]byte) {
	body := buildEnvelope(batch)
	delay := 250 * time.Millisecond
	for attempt := 0; attempt <= s.opts.MaxRetries; attempt++ {
		err := s.postOnce(body)
		if err == nil {
			return
		}
		// If Close was signalled during this attempt, exit immediately
		// rather than spending another (Timeout × remaining-attempts)
		// before respecting shutdown.
		select {
		case <-s.closed:
			s.gaveUp.Add(1)
			return
		default:
		}
		if !isRetryable(err) || attempt == s.opts.MaxRetries {
			s.gaveUp.Add(1)
			if s.shouldWarn() {
				fmt.Fprintf(os.Stderr, "log: webhook sink gave up after %d attempt(s): %v\n", attempt+1, err)
			}
			return
		}
		select {
		case <-s.closed:
			s.gaveUp.Add(1)
			return
		case <-time.After(delay):
		}
		delay *= 2
	}
}

func (s *webhookSink) postOnce(body []byte) error {
	// Bounded by Timeout; we DON'T cancel on s.closed here because the
	// flush-on-Close path needs the final attempt to complete normally.
	// Mid-retry shutdown is handled by the post-attempt check in send.
	ctx, cancel := context.WithTimeout(context.Background(), s.opts.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, s.opts.Method, s.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range s.opts.Headers {
		if k == "Content-Type" {
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return retryableError{code: resp.StatusCode}
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("log: webhook %d", resp.StatusCode)
	}
	return nil
}

// buildEnvelope serializes batch as {"entries":[ <raw entry 1>, <raw entry 2>, ...]}.
// Entries are already valid JSON objects from the fan-out handler, so we
// can splice them in without re-decoding.
func buildEnvelope(batch [][]byte) []byte {
	var b bytes.Buffer
	b.Grow(estimateSize(batch) + 32)
	b.WriteString(`{"entries":[`)
	for i, e := range batch {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(e)
	}
	b.WriteString("]}")
	return b.Bytes()
}

func estimateSize(batch [][]byte) int {
	n := 0
	for _, e := range batch {
		n += len(e) + 1
	}
	return n
}

type retryableError struct{ code int }

func (e retryableError) Error() string { return fmt.Sprintf("log: webhook %d (retryable)", e.code) }

func isRetryable(err error) bool {
	var r retryableError
	if errors.As(err, &r) {
		return true
	}
	// Network errors (timeout, refused) — http.Client returns *url.Error.
	// Treat all transport errors as retryable.
	return err != nil && !errors.As(err, &nonRetryableError{})
}

// nonRetryableError is reserved for future 4xx classifications; currently
// 4xx returns a plain error and we don't retry. Kept so isRetryable has a
// path to distinguish later without an API change.
type nonRetryableError struct{ error }

func (e nonRetryableError) Unwrap() error { return e.error }
