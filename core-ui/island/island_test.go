package island

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// mutableComponent allows changing the rendered HTML between renders.
type mutableComponent struct {
	mu   sync.Mutex
	html render.HTML
}

func (mc *mutableComponent) Render() render.HTML {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	return mc.html
}

func (mc *mutableComponent) Set(html render.HTML) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.html = html
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
	if isl.SessionID != "" {
		t.Errorf("expected empty SessionID, got %q", isl.SessionID)
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

func TestIslandUpdate(t *testing.T) {
	mc := &mutableComponent{html: render.Text("version1")}
	isl := NewIsland("updatable", mc)

	first := string(isl.Update())
	if !strings.Contains(first, "version1") {
		t.Errorf("expected 'version1' in %q", first)
	}

	mc.Set(render.Text("version2"))
	second := string(isl.Update())
	if !strings.Contains(second, "version2") {
		t.Errorf("expected 'version2' in %q", second)
	}
}

func TestManagerRegister(t *testing.T) {
	mgr := NewManager()
	comp := &testComponent{html: render.Text("hello")}
	isl := NewIsland("reg-1", comp)
	isl.SessionID = "sess-1"

	if err := mgr.Register(isl); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	retrieved, ok := mgr.Get("reg-1")
	if !ok {
		t.Fatal("expected to retrieve registered island")
	}
	if retrieved.ID != "reg-1" {
		t.Errorf("expected ID %q, got %q", "reg-1", retrieved.ID)
	}
}

func TestManagerUnregister(t *testing.T) {
	mgr := NewManager()
	comp := &testComponent{html: render.Text("hello")}
	isl := NewIsland("unreg-1", comp)
	isl.SessionID = "sess-1"

	mgr.Register(isl)
	mgr.Unregister("unreg-1")

	_, ok := mgr.Get("unreg-1")
	if ok {
		t.Error("expected island to be removed after unregister")
	}
}

func TestManagerGet(t *testing.T) {
	mgr := NewManager()
	comp := &testComponent{html: render.Text("hello")}
	isl := NewIsland("get-1", comp)
	isl.SessionID = "sess-1"
	mgr.Register(isl)

	retrieved, ok := mgr.Get("get-1")
	if !ok {
		t.Fatal("expected to find island")
	}
	if retrieved.SessionID != "sess-1" {
		t.Errorf("expected SessionID %q, got %q", "sess-1", retrieved.SessionID)
	}
}

func TestManagerGetNotFound(t *testing.T) {
	mgr := NewManager()

	_, ok := mgr.Get("nonexistent")
	if ok {
		t.Error("expected not to find nonexistent island")
	}
}

func TestManagerListBySession(t *testing.T) {
	mgr := NewManager()

	for i := 0; i < 3; i++ {
		comp := &testComponent{html: render.Text("hello")}
		isl := NewIsland("list-"+string(rune('A'+i)), comp)
		isl.SessionID = "sess-list"
		mgr.Register(isl)
	}

	ids := mgr.ListBySession("sess-list")
	if len(ids) != 3 {
		t.Errorf("expected 3 islands, got %d", len(ids))
	}

	// Verify all IDs are present.
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	for _, expected := range []string{"list-A", "list-B", "list-C"} {
		if !set[expected] {
			t.Errorf("expected ID %q in list", expected)
		}
	}
}

func TestManagerPush(t *testing.T) {
	mgr := NewManager()
	mc := &mutableComponent{html: render.Text("initial")}
	isl := NewIsland("push-1", mc)
	isl.SessionID = "sess-push"
	mgr.Register(isl)

	// Subscribe to get updates.
	ch := mgr.Subscribe("sess-push")

	// Push an update.
	if err := mgr.Push("push-1"); err != nil {
		t.Fatalf("unexpected push error: %v", err)
	}

	select {
	case update := <-ch:
		if update.IslandID != "push-1" {
			t.Errorf("expected IslandID %q, got %q", "push-1", update.IslandID)
		}
		if !strings.Contains(update.HTML, "initial") {
			t.Errorf("expected HTML to contain 'initial', got %q", update.HTML)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for update")
	}

	// Change component and push again.
	mc.Set(render.Text("updated"))
	mgr.Push("push-1")

	select {
	case update := <-ch:
		if !strings.Contains(update.HTML, "updated") {
			t.Errorf("expected HTML to contain 'updated', got %q", update.HTML)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second update")
	}
}

func TestManagerSubscribe(t *testing.T) {
	mgr := NewManager()

	ch := mgr.Subscribe("sess-sub")

	if ch == nil {
		t.Fatal("expected non-nil channel")
	}

	// Subscribing again should return the same channel.
	ch2 := mgr.Subscribe("sess-sub")
	if ch != ch2 {
		t.Error("expected same channel for repeated subscribe")
	}
}

func TestManagerUnsubscribe(t *testing.T) {
	mgr := NewManager()

	entry := func() *streamEntry {
		mgr.mu.Lock()
		defer mgr.mu.Unlock()
		return mgr.streams["sess-unsub"]
	}

	ch := mgr.Subscribe("sess-unsub")
	e := entry()
	if e == nil {
		t.Fatal("expected stream entry after subscribe")
	}

	mgr.Unsubscribe("sess-unsub")

	// Entry should be removed from streams map.
	if entry() != nil {
		t.Error("expected entry to be removed from streams after unsubscribe")
	}

	// Data channel should NOT be closed (new design prevents send-on-closed panic).
	// Instead, verify that the done channel is closed.
	select {
	case <-e.done:
		// done channel is closed — correct
	default:
		t.Error("expected done channel to be closed after unsubscribe")
	}

	// Verify that sending after unsubscribe doesn't panic
	mgr.Register(&Island{ID: "test-island", Component: &testComponent{html: render.Text("x")}, SessionID: "sess-unsub"})
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

func TestManagerMultipleIslands(t *testing.T) {
	mgr := NewManager()
	sessionID := "sess-multi"

	comp1 := &testComponent{html: render.Text("island-1")}
	comp2 := &testComponent{html: render.Text("island-2")}
	comp3 := &testComponent{html: render.Text("island-3")}

	isl1 := NewIsland("multi-1", comp1)
	isl1.SessionID = sessionID
	isl2 := NewIsland("multi-2", comp2)
	isl2.SessionID = sessionID
	isl3 := NewIsland("multi-3", comp3)
	isl3.SessionID = sessionID

	mgr.Register(isl1)
	mgr.Register(isl2)
	mgr.Register(isl3)

	ids := mgr.ListBySession(sessionID)
	if len(ids) != 3 {
		t.Errorf("expected 3 islands, got %d", len(ids))
	}

	// Subscribe and push all three.
	ch := mgr.Subscribe(sessionID)

	mgr.Push("multi-1")
	mgr.Push("multi-2")
	mgr.Push("multi-3")

	received := map[string]string{}
	timeout := time.After(2 * time.Second)
	for len(received) < 3 {
		select {
		case update := <-ch:
			received[update.IslandID] = update.HTML
		case <-timeout:
			t.Fatalf("timed out, only received %d updates", len(received))
		}
	}

	for _, id := range []string{"multi-1", "multi-2", "multi-3"} {
		if _, ok := received[id]; !ok {
			t.Errorf("expected update for %q", id)
		}
	}
}

func TestManagerConcurrentAccess(t *testing.T) {
	mgr := NewManager()
	var wg sync.WaitGroup

	// Use unique IDs to avoid collisions
	n := 50

	// Concurrent register with unique IDs.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			comp := &testComponent{html: render.Text(fmt.Sprintf("concurrent-%d", idx))}
			isl := NewIsland(fmt.Sprintf("conc-%d", idx), comp)
			isl.SessionID = "sess-conc"
			mgr.Register(isl)
		}(i)
	}
	wg.Wait()

	ids := mgr.ListBySession("sess-conc")
	if len(ids) != n {
		t.Errorf("expected %d islands, got %d", n, len(ids))
	}

	// Subscribe for push tests.
	ch := mgr.Subscribe("sess-conc")

	// Concurrent push + unregister.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("conc-%d", idx)
			if idx%3 == 0 {
				mgr.Unregister(id)
			} else {
				mgr.Push(id)
			}
		}(i)
	}
	wg.Wait()

	// Drain remaining updates with a short timeout.
	drainTimer := time.NewTimer(100 * time.Millisecond)
	defer drainTimer.Stop()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-drainTimer.C:
			return
		}
	}
}

func TestManagerPushNotFound(t *testing.T) {
	mgr := NewManager()

	err := mgr.Push("nonexistent")
	if err == nil {
		t.Error("expected error when pushing nonexistent island")
	}
}

func TestManagerRegisterDuplicate(t *testing.T) {
	mgr := NewManager()
	comp := &testComponent{html: render.Text("hello")}
	isl := NewIsland("dup-1", comp)
	isl.SessionID = "sess-dup"

	mgr.Register(isl)
	err := mgr.Register(isl)
	if err == nil {
		t.Error("expected error when registering duplicate island")
	}
}

func TestServeSSE(t *testing.T) {
	mgr := NewManager()
	comp := &testComponent{html: render.Text("sse-content")}
	isl := NewIsland("sse-1", comp)
	isl.SessionID = "sess-sse"
	mgr.Register(isl)

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
	mgr.Push("sse-1")

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
	comp := &testComponent{html: render.Text("stream-test")}
	isl := NewIsland("stream-1", comp)
	isl.SessionID = "sess-stream"
	mgr.Register(isl)

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

	mgr.Push("stream-1")

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
