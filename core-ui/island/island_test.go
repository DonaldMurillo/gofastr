package island

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// testComponent is a simple component for testing.
type testComponent struct {
	html render.HTML
}

func (tc *testComponent) Render() render.HTML {
	return tc.html
}

// --- Tests ---

func TestNewIsland(t *testing.T) {
	comp := &testComponent{html: render.Text("hello")}
	isl := NewIsland("test-id", comp)

	if isl.ID != "test-id" {
		t.Errorf("expected ID %q, got %q", "test-id", isl.ID)
	}
	if isl.Component != comp {
		t.Error("expected component to be set")
	}
}

func TestIslandRender(t *testing.T) {
	comp := &testComponent{html: render.Text("hello")}
	isl := NewIsland("my-island", comp)

	got := string(isl.Render())

	// Should have data-island wrapper.
	if !strings.Contains(got, `data-island="my-island"`) {
		t.Errorf("expected data-island attribute in %q", got)
	}
	if !strings.HasPrefix(got, "<div") {
		t.Errorf("expected <div wrapper, got %q", got)
	}
	if !strings.Contains(got, "hello") {
		t.Errorf("expected component content in %q", got)
	}
	if !strings.HasSuffix(got, "</div>") {
		t.Errorf("expected closing </div>, got %q", got)
	}
}

func TestManagerSubscribe(t *testing.T) {
	mgr := NewManager()

	ch, cancel := mgr.Subscribe("sess-sub")
	defer cancel()

	if ch == nil {
		t.Fatal("expected non-nil channel")
	}

	// A second subscriber (another tab) gets its OWN channel.
	ch2, cancel2 := mgr.Subscribe("sess-sub")
	defer cancel2()
	if ch == ch2 {
		t.Error("expected a private channel per subscriber")
	}
}

func TestTwoTabsBothReceiveUpdates(t *testing.T) {
	mgr := NewManager()
	a, cancelA := mgr.Subscribe("sess-1")
	defer cancelA()
	b, cancelB := mgr.Subscribe("sess-1")
	defer cancelB()

	mgr.PushUpdate(IslandUpdate{IslandID: "x", HTML: "<p>hi</p>"}, "sess-1")

	for name, ch := range map[string]<-chan IslandUpdate{"tab A": a, "tab B": b} {
		select {
		case u := <-ch:
			if u.IslandID != "x" {
				t.Fatalf("%s got %q", name, u.IslandID)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s never received the update", name)
		}
	}
}

func TestManagerUnsubscribe(t *testing.T) {
	mgr := NewManager()

	entry := func() *streamEntry {
		mgr.mu.Lock()
		defer mgr.mu.Unlock()
		return mgr.streams["sess-unsub"]
	}

	ch, cancel := mgr.Subscribe("sess-unsub")
	if entry() == nil {
		t.Fatal("expected stream entry after subscribe")
	}

	cancel()

	// Last subscriber cancelled → entry removed from streams map.
	if entry() != nil {
		t.Error("expected entry to be removed from streams after cancel")
	}

	// cancel is idempotent.
	cancel()

	// PushUpdate should not panic even though unsubscribed
	mgr.PushUpdate(IslandUpdate{IslandID: "test-island", HTML: "x"}, "sess-unsub")

	// The data channel should be usable without blocking or panicking
	select {
	case <-ch:
		t.Error("data channel should not receive after unsubscribe")
	default:
		// correct — no data sent because stream was removed
	}
}

func TestServeSSE(t *testing.T) {
	mgr := NewManager()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mgr.ServeSSE(w, r)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	// Use a cancellable context so we can close the SSE connection after reading.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"?session=sess-sse", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to connect SSE: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %q", ct)
	}

	// Push an update — should arrive on the SSE stream.
	mgr.PushUpdate(IslandUpdate{IslandID: "sse-1", HTML: "<p>sse-content</p>"}, "sess-sse")

	// Read response body in a goroutine, looping until we get island data.
	bodyCh := make(chan string, 1)
	go func() {
		var buf strings.Builder
		tmp := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(tmp)
			if n > 0 {
				buf.Write(tmp[:n])
			}
			if strings.Contains(buf.String(), "event: island") || err != nil {
				bodyCh <- buf.String()
				return
			}
		}
	}()

	select {
	case body := <-bodyCh:
		if !strings.Contains(body, "event: island") {
			t.Errorf("expected 'event: island' in SSE output, got %q", body)
		}

		idx := strings.Index(body, "data: ")
		if idx < 0 {
			t.Fatal("expected 'data: ' in SSE output")
		}
		jsonStr := body[idx+6:]
		if nl := strings.Index(jsonStr, "\n"); nl >= 0 {
			jsonStr = jsonStr[:nl]
		}

		var payload ssePayload
		if err := json.Unmarshal([]byte(jsonStr), &payload); err != nil {
			t.Fatalf("failed to parse SSE payload: %v\njson: %q", err, jsonStr)
		}
		if payload.Island != "sse-1" {
			t.Errorf("expected island %q, got %q", "sse-1", payload.Island)
		}
		if !strings.Contains(payload.HTML, "sse-content") {
			t.Errorf("expected HTML to contain 'sse-content', got %q", payload.HTML)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for SSE data")
	}

	// Cancel context to close the SSE connection.
	cancel()
}

func TestServeSSEMissingSession(t *testing.T) {
	mgr := NewManager()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	mgr.ServeSSE(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestServeSSEUsesStreamPackage(t *testing.T) {
	// Verify that ServeSSE produces output compatible with stream.Event.
	mgr := NewManager()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mgr.ServeSSE(w, r)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"?session=sess-stream", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer resp.Body.Close()

	mgr.PushUpdate(IslandUpdate{IslandID: "stream-1", HTML: "<p>stream-test</p>"}, "sess-stream")

	bodyCh := make(chan string, 1)
	go func() {
		var buf strings.Builder
		tmp := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(tmp)
			if n > 0 {
				buf.Write(tmp[:n])
			}
			if strings.Contains(buf.String(), "event: island") || err != nil {
				bodyCh <- buf.String()
				return
			}
		}
	}()

	select {
	case body := <-bodyCh:
		if !strings.Contains(body, "event: island\n") {
			t.Errorf("expected 'event: island\\n' format, got %q", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for SSE data")
	}

	cancel()
}

// Verify the test type matches the internal one.
var _ = json.Marshal
