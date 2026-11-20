package log

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/backoff"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// WebhookOpts configures a webhook sink.
type WebhookOpts struct {
	// Method defaults to "POST".
	Method string

	// Headers are appended to every request.
	Headers map[string]string

	// BatchSize triggers a flush.
	BatchSize int
	// BatchInterval triggers a flush regardless of size.
	BatchInterval time.Duration
	// QueueSize caps the in-memory buffer.
	QueueSize int

	// Timeout for each HTTP request.
	Timeout time.Duration
	// MaxRetries on transient failure (5xx, network).
	MaxRetries int

	// HTTPClient lets tests inject a stub.
	HTTPClient *http.Client
}

type webhookSink struct {
	url    string
	opts   WebhookOpts
	client *http.Client

	mu        sync.Mutex
	queue     [][]byte
	flush     chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
	done      chan struct{}

	dropped atomic.Uint64
	gaveUp  atomic.Uint64

	lastWarnAt   atomic.Int64
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
	cp := *client
	cp.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	client = &cp
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
	select {
	case <-s.closed:
		s.mu.Unlock()
		return ErrSinkClosed
	default:
	}
	if len(s.queue) >= s.opts.QueueSize {
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
// queue was full.
func (s *webhookSink) Dropped() uint64 { return s.dropped.Load() }

// GaveUp returns the running count of batches given up after exhausting
// MaxRetries, log volume that NEVER reached the downstream receiver.
func (s *webhookSink) GaveUp() uint64 { return s.gaveUp.Load() }

// Close signals the worker goroutine to flush + exit and waits for it.
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
	for attempt := 0; attempt <= s.opts.MaxRetries; attempt++ {
		err := s.postOnce(body)
		if err == nil {
			return
		}
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
		case <-time.After(backoff.Exponential(250*time.Millisecond, 0, attempt+1)):
		}
	}
}

func (s *webhookSink) postOnce(body []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = nonRetryableError{fmt.Errorf("log: webhook delivery panicked: %s", textsafe.Recovered(r))}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), s.opts.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, s.opts.Method, s.url, bytes.NewReader(body))
	if err != nil {
		return redactURLError(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range s.opts.Headers {
		if http.CanonicalHeaderKey(k) == "Content-Type" {
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return redactURLError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return retryableError{code: resp.StatusCode}
	}
	if resp.StatusCode >= 300 {
		return nonRetryableError{fmt.Errorf("log: webhook %d", resp.StatusCode)}
	}
	return nil
}

func redactURLError(err error) error {
	ue, ok := errors.AsType[*url.Error](err)
	if !ok {
		return err
	}
	return &url.Error{Op: ue.Op, URL: redactURL(ue.URL), Err: ue.Err}
}

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "(webhook url redacted)"
	}
	return u.Scheme + "://" + u.Host + "/…"
}

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
	if _, ok := errors.AsType[retryableError](err); ok {
		return true
	}
	return err != nil && !errors.As(err, &nonRetryableError{})
}

type nonRetryableError struct{ error }

func (e nonRetryableError) Unwrap() error { return e.error }
