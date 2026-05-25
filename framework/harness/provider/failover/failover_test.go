package failover

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
)

type fakeProv struct {
	name string
	err  error
	text string
	calls int
}

func (f *fakeProv) Name() string { return f.name }
func (f *fakeProv) Chat(_ context.Context, _ *provider.Request) (<-chan provider.StreamEvent, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Kind: provider.KindTextDelta, Text: f.text}
	ch <- provider.StreamEvent{Kind: provider.KindStop}
	close(ch)
	return ch, nil
}
func (f *fakeProv) Models(_ context.Context) ([]provider.Model, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []provider.Model{{ID: f.name + "/m"}}, nil
}
func (f *fakeProv) TokenCount(_ context.Context, _ string, _ []provider.Message) (int, error) {
	return 0, nil
}

func TestFailoverPicksFirstHealthy(t *testing.T) {
	a := &fakeProv{name: "a", err: errors.New("HTTP 503 service unavailable")}
	b := &fakeProv{name: "b", text: "from-b"}
	c := New(a, b)

	stream, err := c.Chat(context.Background(), &provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for ev := range stream {
		if ev.Kind == provider.KindTextDelta {
			out.WriteString(ev.Text)
		}
	}
	if out.String() != "from-b" {
		t.Errorf("text = %q", out.String())
	}
	if a.calls != 1 || b.calls != 1 {
		t.Errorf("calls: a=%d b=%d", a.calls, b.calls)
	}
}

func TestFailoverPropagatesNonRetryable(t *testing.T) {
	a := &fakeProv{name: "a", err: errors.New("HTTP 401 unauthorized")}
	b := &fakeProv{name: "b", text: "from-b"}
	c := New(a, b)
	_, err := c.Chat(context.Background(), &provider.Request{})
	if err == nil {
		t.Fatal("expected 401 to propagate")
	}
	if b.calls != 0 {
		t.Errorf("non-retryable should not advance to b: b.calls=%d", b.calls)
	}
}

func TestBreakerOpensAfterThreshold(t *testing.T) {
	a := &fakeProv{name: "a", err: errors.New("HTTP 500 boom")}
	b := &fakeProv{name: "b", text: "from-b"}
	c := New(a, b)
	c.FailureThreshold = 2
	c.CooldownDuration = time.Hour

	// First call: a fails, b succeeds. a's breaker has 1 failure.
	stream, _ := c.Chat(context.Background(), &provider.Request{})
	for range stream {
	}
	if a.calls != 1 {
		t.Fatalf("a.calls = %d, want 1", a.calls)
	}

	// Second call: a fails again (threshold reached), breaker opens.
	stream, _ = c.Chat(context.Background(), &provider.Request{})
	for range stream {
	}
	if a.calls != 2 {
		t.Fatalf("a.calls = %d, want 2", a.calls)
	}

	// Third call: a's breaker is open within cooldown; skip a.
	preB := b.calls
	stream, _ = c.Chat(context.Background(), &provider.Request{})
	for range stream {
	}
	if a.calls != 2 {
		t.Errorf("a should be skipped while breaker open; got a.calls=%d", a.calls)
	}
	if b.calls != preB+1 {
		t.Errorf("b.calls = %d, want %d", b.calls, preB+1)
	}
}

func TestNoMembersAvailable(t *testing.T) {
	c := New()
	_, err := c.Chat(context.Background(), &provider.Request{})
	if !errors.Is(err, ErrNoMembersAvailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestIsRetryable(t *testing.T) {
	retryable := []string{
		"openrouter: HTTP 500",
		"zai: HTTP 503 something",
		"copilot: HTTP 429 rate limited",
		"copilot: HTTP 408 request timeout",
		"network: connection refused",
		"io: EOF",
	}
	for _, m := range retryable {
		if !isRetryable(errors.New(m)) {
			t.Errorf("isRetryable(%q) = false", m)
		}
	}
	notRetryable := []string{
		"HTTP 401 unauthorized",
		"HTTP 400 bad request",
		"HTTP 404 not found",
	}
	for _, m := range notRetryable {
		if isRetryable(errors.New(m)) {
			t.Errorf("isRetryable(%q) = true", m)
		}
	}
}
