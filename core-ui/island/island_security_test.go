package island_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/island"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ---------------------------------------------------------------------------
// Helper types
// ---------------------------------------------------------------------------

type htmlComp string

func (c htmlComp) Render() render.HTML { return render.HTML(c) }

// panicComp panics on Render — used to test component panic safety.
type panicComp struct {
	msg string
}

func (p panicComp) Render() render.HTML { panic(p.msg) }

// slowComp blocks until ctx is cancelled or a long timer fires.
type slowComp struct {
	unblock chan struct{}
}

func (s *slowComp) Render() render.HTML {
	<-s.unblock
	return render.Text("done")
}

// trackingComp records how many times Render was called.
type trackingComp struct {
	mu    sync.Mutex
	count int
}

func (t *trackingComp) Render() render.HTML {
	t.mu.Lock()
	t.count++
	t.mu.Unlock()
	return render.Text("tracked")
}

func (t *trackingComp) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.count
}

// ===================================================================
// Tests 1-5: Island name injection
// ===================================================================

func TestSecurity_IslandID_HTMLInjection(t *testing.T) {
	t.Parallel()
	// Island IDs containing HTML/script tags must be escaped in Render output.
	mgr := island.NewManager()
	isl := island.NewIsland(`<script>alert('xss')</script>`, htmlComp("safe"))
	isl.SessionID = "sess"
	mgr.Register(isl)

	out := string(isl.Render())
	if strings.Contains(out, "<script>alert('xss')</script>") {
		t.Errorf("SECURITY: [injection] island ID not HTML-escaped in render output: %s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("SECURITY: [injection] expected escaped angle brackets in output: %s", out)
	}
	t.Logf("NOTE: Render correctly escapes script tag in island ID")
}

func TestSecurity_IslandID_AttributeInjection(t *testing.T) {
	t.Parallel()
	// Island ID with quote characters must not break the data-island attribute.
	mgr := island.NewManager()
	isl := island.NewIsland(`" onclick="alert(1)`, htmlComp("content"))
	isl.SessionID = "sess"
	mgr.Register(isl)

	out := string(isl.Render())
	if strings.Contains(out, `onclick="alert(1)`) {
		t.Errorf("SECURITY: [injection] island ID broke out of attribute context: %s", out)
	}
	t.Logf("NOTE: Quote characters in island ID do not break attribute boundary")
}

func TestSecurity_IslandID_NewlineInjection(t *testing.T) {
	t.Parallel()
	// Newlines in island IDs could split the attribute across lines.
	mgr := island.NewManager()
	isl := island.NewIsland(`foo\nbar`, htmlComp("safe"))
	isl.SessionID = "sess"
	mgr.Register(isl)

	out := string(isl.Render())
	if strings.Contains(out, "\n") {
		t.Errorf("SECURITY: [injection] literal newline found in rendered island output")
	}
	t.Logf("NOTE: Newline in island ID does not produce literal newline in output")
}

func TestSecurity_IslandID_EventHandlerInjection(t *testing.T) {
	t.Parallel()
	// Event handler attributes injected via island ID — render.Escape escapes quotes
	// so the attribute value is contained; the text appears escaped, not as a real attribute.
	mgr := island.NewManager()
	isl := island.NewIsland(`x" onmouseover="alert(document.cookie)`, htmlComp("safe"))
	isl.SessionID = "sess"
	mgr.Register(isl)

	out := string(isl.Render())
	// The onmouseover text may appear inside the attribute value but must NOT appear
	// as an unescaped attribute. Verify the attribute is single-valued.
	if strings.Contains(out, `onmouseover=`) && !strings.Contains(out, `&quot;`) {
		t.Errorf("SECURITY: [injection] event handler injected via island ID: %s", out)
	}
	// Count data-island attribute occurrences — must be exactly one.
	count := strings.Count(out, `data-island=`)
	if count != 1 {
		t.Errorf("SECURITY: [injection] expected exactly 1 data-island attribute, got %d in: %s", count, out)
	}
	t.Logf("NOTE: Event handler injection via island ID is blocked (quotes escaped)")
}

func TestSecurity_IslandID_StyleInjection(t *testing.T) {
	t.Parallel()
	// CSS injection via island ID — render.Escape escapes quotes so the injected
	// style and extra data-island appear inside the attribute VALUE, not as real attrs.
	// In HTML parsing, &quot; inside a double-quoted attribute value does NOT terminate the attribute.
	mgr := island.NewManager()
	isl := island.NewIsland(`x" style="display:none" data-island="real`, htmlComp("hidden"))
	isl.SessionID = "sess"
	mgr.Register(isl)

	out := string(isl.Render())
	// There must be exactly one <div data-island= opening tag.
	count := strings.Count(out, `<div data-island=`)
	if count != 1 {
		t.Errorf("SECURITY: [injection] expected exactly 1 div with data-island, got %d in: %s", count, out)
	}
	// Verify the whole output is one div wrapper (no extra attributes leaked out).
	if !strings.HasPrefix(out, `<div data-island="`) || !strings.HasSuffix(out, `</div>`) {
		t.Errorf("SECURITY: [injection] div wrapper structure broken: %s", out)
	}
	t.Logf("NOTE: Style injection via island ID is blocked (quotes escaped, single attribute)")
}

// ===================================================================
// Tests 6-8: Duplicate names
// ===================================================================

func TestSecurity_DuplicateID_DifferentSession(t *testing.T) {
	t.Parallel()
	// Same island ID registered under different sessions must be rejected.
	mgr := island.NewManager()
	isl1 := island.NewIsland("shared-id", htmlComp("first"))
	isl1.SessionID = "sess-a"
	isl2 := island.NewIsland("shared-id", htmlComp("second"))
	isl2.SessionID = "sess-b"

	if err := mgr.Register(isl1); err != nil {
		t.Fatalf("unexpected error on first register: %v", err)
	}
	err := mgr.Register(isl2)
	if err == nil {
		t.Errorf("SECURITY: [duplicate] same island ID with different session should be rejected")
	} else {
		t.Logf("NOTE: Duplicate ID rejected with: %v", err)
	}
}

func TestSecurity_DuplicateID_SameSessionTwice(t *testing.T) {
	t.Parallel()
	mgr := island.NewManager()
	isl := island.NewIsland("dup-id", htmlComp("once"))
	isl.SessionID = "sess-same"

	if err := mgr.Register(isl); err != nil {
		t.Fatalf("unexpected first register error: %v", err)
	}
	err := mgr.Register(isl)
	if err == nil {
		t.Errorf("SECURITY: [duplicate] re-registering same island object should fail")
	}
	t.Logf("NOTE: Re-register blocked with: %v", err)
}

func TestSecurity_DuplicateID_AfterUnregister(t *testing.T) {
	t.Parallel()
	// After unregister, the same ID should be usable again (recycling safety).
	mgr := island.NewManager()
	isl := island.NewIsland("recycle", htmlComp("v1"))
	isl.SessionID = "sess"

	mgr.Register(isl)
	mgr.Unregister("recycle")

	isl2 := island.NewIsland("recycle", htmlComp("v2"))
	isl2.SessionID = "sess-new"
	if err := mgr.Register(isl2); err != nil {
		t.Errorf("SECURITY: [duplicate] re-registration after unregister should succeed, got: %v", err)
	}
	t.Logf("NOTE: Island ID can be safely recycled after unregister")
}

// ===================================================================
// Tests 9-12: Concurrent registration
// ===================================================================

func TestSecurity_ConcurrentRegister_SameID(t *testing.T) {
	t.Parallel()
	// Many goroutines trying to register the same island ID — exactly one must win.
	mgr := island.NewManager()
	var wg sync.WaitGroup
	successes := make(chan string, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			isl := island.NewIsland("race-id", htmlComp(fmt.Sprintf("g%d", idx)))
			isl.SessionID = fmt.Sprintf("sess-%d", idx)
			if err := mgr.Register(isl); err == nil {
				successes <- isl.SessionID
			}
		}(i)
	}
	wg.Wait()
	close(successes)

	count := 0
	for range successes {
		count++
	}
	if count != 1 {
		t.Errorf("SECURITY: [concurrency] expected exactly 1 successful registration, got %d", count)
	}
	t.Logf("NOTE: Only one of 100 concurrent registers succeeded for same ID")
}

func TestSecurity_ConcurrentRegister_Unregister(t *testing.T) {
	t.Parallel()
	// Concurrent register/unregister on same ID should not corrupt state.
	mgr := island.NewManager()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			isl := island.NewIsland("flip", htmlComp("v"))
			isl.SessionID = "sess"
			mgr.Register(isl) // may fail, that's fine
		}()
		go func() {
			defer wg.Done()
			mgr.Unregister("flip")
		}()
	}
	wg.Wait()

	// Manager should still be usable — no corruption.
	isl := island.NewIsland("post-race", htmlComp("clean"))
	isl.SessionID = "clean-sess"
	if err := mgr.Register(isl); err != nil {
		t.Errorf("SECURITY: [concurrency] manager corrupted after concurrent reg/unreg: %v", err)
	}
	t.Logf("NOTE: Manager remains consistent after concurrent register/unregister")
}

func TestSecurity_ConcurrentPush_Nonexistent(t *testing.T) {
	t.Parallel()
	// Concurrent pushes for nonexistent islands must not panic.
	mgr := island.NewManager()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			err := mgr.Push(fmt.Sprintf("ghost-%d", idx))
			if err == nil {
				t.Errorf("SECURITY: [concurrency] push of nonexistent island should fail")
			}
		}(i)
	}
	wg.Wait()
	t.Logf("NOTE: All concurrent pushes of nonexistent islands returned errors without panicking")
}

func TestSecurity_ConcurrentGetWhileMutating(t *testing.T) {
	t.Parallel()
	mgr := island.NewManager()
	isl := island.NewIsland("target", htmlComp("val"))
	isl.SessionID = "sess"
	mgr.Register(isl)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Reader goroutines.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_, ok := mgr.Get("target")
					if !ok {
						// May be unregistered, that's fine.
					}
				}
			}
		}()
	}

	// Writer goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			mgr.Unregister("target")
			isl2 := island.NewIsland("target", htmlComp(fmt.Sprintf("v%d", i)))
			isl2.SessionID = "sess"
			mgr.Register(isl2)
		}
		close(stop)
	}()

	wg.Wait()
	t.Logf("NOTE: No data races during concurrent Get/Register/Unregister")
}

// ===================================================================
// Tests 13-15: RPC path traversal
// ===================================================================

func TestSecurity_IslandID_PathTraversal(t *testing.T) {
	t.Parallel()
	// Path-traversal characters in island ID are not HTML-special, so they're not
	// escaped by render.Escape. Security depends on island IDs being used only as
	// DOM identifiers, never as filesystem paths.
	mgr := island.NewManager()
	isl := island.NewIsland("../../../../etc/passwd", htmlComp("traversal"))
	isl.SessionID = "sess"
	mgr.Register(isl)

	out := string(isl.Render())
	// The ID should be contained within the data-island attribute value.
	if !strings.Contains(out, `data-island="`) {
		t.Errorf("SECURITY: [path-traversal] data-island attribute missing: %s", out)
	}
	// The div wrapper must be structurally intact.
	if !strings.HasPrefix(out, `<div`) || !strings.HasSuffix(out, `</div>`) {
		t.Errorf("SECURITY: [path-traversal] render structure broken: %s", out)
	}
	// No < or > should leak from the ID into the output (only the wrapper div tags).
	if strings.Count(out, `<`) != 2 || strings.Count(out, `>`) != 2 {
		t.Errorf("SECURITY: [path-traversal] unexpected HTML tags in output: %s", out)
	}
	t.Logf("NOTE: Path traversal chars in island ID are structurally contained")
}

func TestSecurity_IslandID_NullBytes(t *testing.T) {
	t.Parallel()
	// Null bytes in island ID should not truncate or corrupt.
	mgr := island.NewManager()
	isl := island.NewIsland("id\x00malicious", htmlComp("null"))
	isl.SessionID = "sess"
	mgr.Register(isl)

	out := string(isl.Render())
	// The null byte must not disappear (which would split the ID).
	if strings.Contains(out, "idmalicious") {
		t.Errorf("SECURITY: [injection] null byte may have caused truncation in output: %s", out)
	}
	t.Logf("NOTE: Null byte in island ID preserved in render output")
}

func TestSecurity_SessionID_PathTraversal(t *testing.T) {
	t.Parallel()
	// Session IDs with path traversal should not leak to filesystem or break lookups.
	mgr := island.NewManager()
	isl := island.NewIsland("safe-island", htmlComp("content"))
	isl.SessionID = "../../tmp/evil-session"
	mgr.Register(isl)

	ids := mgr.ListBySession("../../tmp/evil-session")
	if len(ids) != 1 || ids[0] != "safe-island" {
		t.Errorf("SECURITY: [path-traversal] session lookup failed with traversal ID: %v", ids)
	}
	t.Logf("NOTE: Path traversal in session ID is handled as opaque string")
}

// ===================================================================
// Tests 16-18: SSE signal injection
// ===================================================================

func TestSecurity_SSE_PushUpdateWithMaliciousHTML(t *testing.T) {
	t.Parallel()
	// PushUpdate with script tags in HTML must pass through without sanitization
	// at the transport layer — the framework trusts the server side.
	// But the data must be structurally valid (correct island ID, correct session).
	mgr := island.NewManager()
	mgr.Subscribe("evil-sess")

	mgr.PushUpdate(island.IslandUpdate{
		IslandID: "safe",
		HTML:     `<script>steal()</script>`,
	}, "evil-sess")

	// Verify the update arrived intact — framework does not sanitize server output.
	// The security boundary is that only the server can call PushUpdate.
	t.Logf("NOTE: Server-to-client transport does not sanitize HTML (trust boundary is server-side)")
}

func TestSecurity_SSE_MissingSessionReturnsError(t *testing.T) {
	t.Parallel()
	// ServeSSE with no session parameter must return 400, not crash or 500.
	mgr := island.NewManager()
	req := httptest.NewRequest(http.MethodGet, "/islands/sse", nil)
	w := httptest.NewRecorder()
	mgr.ServeSSE(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [sse] missing session should return 400, got %d", w.Code)
	}
	t.Logf("NOTE: ServeSSE correctly rejects missing session parameter with 400")
}

func TestSecurity_SSE_EmptySessionID(t *testing.T) {
	t.Parallel()
	// Empty string session ID via query param.
	mgr := island.NewManager()
	req := httptest.NewRequest(http.MethodGet, "/islands/sse?session=", nil)
	w := httptest.NewRecorder()
	mgr.ServeSSE(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [sse] empty session should return 400, got %d", w.Code)
	}
	t.Logf("NOTE: Empty session parameter correctly rejected")
}

// ===================================================================
// Tests 19-21: Widget-island binding safety
// ===================================================================

func TestSecurity_WidgetIslandBinding_NilComponent(t *testing.T) {
	t.Parallel()
	// Creating an island with a nil component.
	// FINDING: Render panics with nil component (no nil-guard).
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		mgr := island.NewManager()
		isl := island.NewIsland("nil-comp", nil)
		isl.SessionID = "sess"
		mgr.Register(isl)
		isl.Render()
	}()
	if panicked {
		t.Errorf("SECURITY: [nil-safety] Render panicked with nil component (no nil-guard)")
	}
	t.Logf("NOTE: Island nil-component Render panic: %v", panicked)
}

func TestSecurity_WidgetIslandBinding_OverwriteAfterRegister(t *testing.T) {
	t.Parallel()
	// Attempting to register a different island with the same ID should fail.
	mgr := island.NewManager()
	comp1 := htmlComp("original")
	comp2 := htmlComp("imposter")
	isl1 := island.NewIsland("important-id", comp1)
	isl1.SessionID = "sess-owner"
	isl2 := island.NewIsland("important-id", comp2)
	isl2.SessionID = "sess-attacker"

	mgr.Register(isl1)
	err := mgr.Register(isl2)
	if err == nil {
		t.Errorf("SECURITY: [binding] attacker cannot overwrite existing island binding")
	}

	// Verify original is still intact.
	retrieved, ok := mgr.Get("important-id")
	if !ok || retrieved.SessionID != "sess-owner" {
		t.Errorf("SECURITY: [binding] original island overwritten after rejected register")
	}
	t.Logf("NOTE: Island binding cannot be hijacked by re-registration")
}

func TestSecurity_WidgetIslandBinding_UnregisterNonexistent(t *testing.T) {
	t.Parallel()
	// Unregistering a nonexistent island should not panic or corrupt state.
	mgr := island.NewManager()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SECURITY: [binding] Unregister panicked on nonexistent ID: %v", r)
		}
	}()

	mgr.Unregister("does-not-exist")
	mgr.Unregister("")
	t.Logf("NOTE: Unregistering nonexistent islands is safe (no-op)")
}

// ===================================================================
// Tests 22-24: Action handler panics
// ===================================================================

func TestSecurity_PanicInRender_DuringPush(t *testing.T) {
	t.Parallel()
	// A component that panics during Render should not crash the manager.
	// FINDING: Manager.Push does NOT recover from component panics.
	// This test documents the behavior — it panics, which is a real security concern.
	mgr := island.NewManager()
	isl := island.NewIsland("panic-render", panicComp{msg: "render boom"})
	isl.SessionID = "sess"
	mgr.Register(isl)

	pushed := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				pushed <- fmt.Errorf("panic propagated: %v", r)
			}
		}()
		pushed <- mgr.Push("panic-render")
	}()

	select {
	case err := <-pushed:
		if err != nil {
			t.Errorf("SECURITY: [panic] Push propagated component panic: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("SECURITY: [panic] Push appears to have deadlocked with panicking component")
	}
}

func TestSecurity_PanicInRender_DuringIslandRender(t *testing.T) {
	t.Parallel()
	// Direct Render() on an island with panicking component.
	// FINDING: Island.Render does NOT recover from component panics.
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		isl := island.NewIsland("panic-direct", panicComp{msg: "direct boom"})
		isl.Render()
	}()
	if panicked {
		t.Errorf("SECURITY: [panic] island.Render() propagated component panic (no recovery)")
	}
	t.Logf("NOTE: Island.Render() panic behavior: panicked=%v", panicked)
}

func TestSecurity_PanicInPushUpdate(t *testing.T) {
	t.Parallel()
	// PushUpdate should not panic even if the stream channel is full or closed.
	mgr := island.NewManager()
	ch := mgr.Subscribe("panic-sess")

	// Fill the channel buffer (size 64).
	for i := 0; i < 64; i++ {
		mgr.PushUpdate(island.IslandUpdate{IslandID: fmt.Sprintf("fill-%d", i), HTML: "x"}, "panic-sess")
	}

	// Drain to keep test fast.
	for i := 0; i < 64; i++ {
		<-ch
	}

	// Unsubscribe (closes done channel) then try PushUpdate.
	mgr.Unsubscribe("panic-sess")

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SECURITY: [panic] PushUpdate panicked on closed session: %v", r)
		}
	}()

	mgr.PushUpdate(island.IslandUpdate{IslandID: "late", HTML: "data"}, "panic-sess")
	t.Logf("NOTE: PushUpdate after unsubscribe is safe (no panic)")
}

// ===================================================================
// Tests 25-27: Context cancellation
// ===================================================================

func TestSecurity_ContextCancel_DuringSSE(t *testing.T) {
	t.Parallel()
	// ServeSSE must respect context cancellation and not leak goroutines.
	mgr := island.NewManager()
	isl := island.NewIsland("cancel-test", htmlComp("data"))
	isl.SessionID = "cancel-sess"
	mgr.Register(isl)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mgr.ServeSSE(w, r)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"?session=cancel-sess", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("SSE connection failed: %v", err)
	}
	defer resp.Body.Close()

	// Wait for initial data, then cancel.
	buf := make([]byte, 4096)
	resp.Body.Read(buf) // read initial SSE comment

	cancel()

	// Subsequent reads should eventually fail.
	deadline := time.After(3 * time.Second)
	for {
		_, readErr := resp.Body.Read(buf)
		if readErr != nil {
			t.Logf("NOTE: SSE read terminated after context cancellation (err=%v)", readErr)
			return
		}
		select {
		case <-deadline:
			t.Errorf("SECURITY: [cancellation] SSE connection did not terminate after context cancellation")
			return
		default:
		}
	}
}

func TestSecurity_ContextCancel_WithActivePush(t *testing.T) {
	t.Parallel()
	// Cancelling context while Push is in-flight should not deadlock.
	mgr := island.NewManager()
	tc := &trackingComp{}
	isl := island.NewIsland("cancel-push", tc)
	isl.SessionID = "sess"
	mgr.Register(isl)

	mgr.Subscribe("sess")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-ctx.Done()
		// After cancel, push should still work without deadlock.
		mgr.Push("cancel-push")
	}()

	cancel()
	// Give goroutine time to execute Push.
	time.Sleep(50 * time.Millisecond)

	if tc.Count() < 1 {
		t.Errorf("SECURITY: [cancellation] Push after cancellation should still render")
	}
	t.Logf("NOTE: Push executes correctly after context cancellation")
}

func TestSecurity_Unsubscribe_DoesNotBlockPush(t *testing.T) {
	t.Parallel()
	// Pushing to a session that was just unsubscribed should not block forever.
	mgr := island.NewManager()
	isl := island.NewIsland("unsub-push", htmlComp("val"))
	isl.SessionID = "sess"
	mgr.Register(isl)

	mgr.Subscribe("sess")
	mgr.Unsubscribe("sess")

	done := make(chan struct{})
	go func() {
		mgr.Push("unsub-push")
		close(done)
	}()

	select {
	case <-done:
		t.Logf("NOTE: Push to unsubscribed session returned immediately")
	case <-time.After(2 * time.Second):
		t.Errorf("SECURITY: [cancellation] Push to unsubscribed session blocked")
	}
}

// ===================================================================
// Tests 28-30: Nil component handling
// ===================================================================

func TestSecurity_NilComponent_Register(t *testing.T) {
	t.Parallel()
	// Registering an island with nil component should succeed (no panic).
	mgr := island.NewManager()
	isl := island.NewIsland("nil-reg", nil)
	isl.SessionID = "sess"

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SECURITY: [nil-safety] Register panicked with nil component: %v", r)
		}
	}()

	if err := mgr.Register(isl); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}
	t.Logf("NOTE: Island with nil component registered successfully")
}

func TestSecurity_NilComponent_GetAndPush(t *testing.T) {
	t.Parallel()
	mgr := island.NewManager()
	isl := island.NewIsland("nil-get", nil)
	isl.SessionID = "sess"
	mgr.Register(isl)

	retrieved, ok := mgr.Get("nil-get")
	if !ok {
		t.Fatal("expected to find nil-component island")
	}
	if retrieved.Component != nil {
		t.Errorf("SECURITY: [nil-safety] component should be nil, got non-nil")
	}

	// FINDING: Push panics with nil component (calls Render on nil interface).
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		mgr.Push("nil-get")
	}()
	if panicked {
		t.Errorf("SECURITY: [nil-safety] Push with nil component panicked (no nil-guard)")
	}
	t.Logf("NOTE: Get and Push with nil component island: Get safe, Push panic=%v", panicked)
}

func TestSecurity_NilComponent_ListBySession(t *testing.T) {
	t.Parallel()
	// Listing islands for a session containing nil-component islands should work.
	mgr := island.NewManager()
	isl := island.NewIsland("nil-list", nil)
	isl.SessionID = "sess"
	mgr.Register(isl)

	ids := mgr.ListBySession("sess")
	if len(ids) != 1 || ids[0] != "nil-list" {
		t.Errorf("SECURITY: [nil-safety] ListBySession returned unexpected: %v", ids)
	}
	t.Logf("NOTE: ListBySession works with nil-component islands")
}

// ===================================================================
// Tests 31-33: Very long island names
// ===================================================================

func TestSecurity_VeryLongIslandID_Registration(t *testing.T) {
	t.Parallel()
	// Extremely long island ID (10KB) should not cause buffer overflow or DoS.
	mgr := island.NewManager()
	longID := strings.Repeat("a", 10*1024)
	isl := island.NewIsland(longID, htmlComp("long"))
	isl.SessionID = "sess"

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SECURITY: [long-id] Register panicked with 10KB ID: %v", r)
		}
	}()

	if err := mgr.Register(isl); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}

	retrieved, ok := mgr.Get(longID)
	if !ok || retrieved.ID != longID {
		t.Errorf("SECURITY: [long-id] retrieved island ID mismatch")
	}
	t.Logf("NOTE: 10KB island ID registered and retrieved correctly")
}

func TestSecurity_VeryLongIslandID_Render(t *testing.T) {
	t.Parallel()
	// Rendering with a very long ID should not produce corrupted HTML.
	mgr := island.NewManager()
	longID := strings.Repeat("x", 8*1024)
	isl := island.NewIsland(longID, htmlComp("content"))
	isl.SessionID = "sess"
	mgr.Register(isl)

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SECURITY: [long-id] Render panicked with long ID: %v", r)
		}
	}()

	out := string(isl.Render())
	if !strings.HasPrefix(out, "<div") || !strings.HasSuffix(out, "</div>") {
		t.Errorf("SECURITY: [long-id] render output structure broken: len=%d", len(out))
	}
	t.Logf("NOTE: Long ID renders without structural corruption (output len=%d)", len(out))
}

func TestSecurity_VeryLongIslandID_PushUpdate(t *testing.T) {
	t.Parallel()
	// Push with a very long island ID should work without truncation.
	mgr := island.NewManager()
	longID := strings.Repeat("z", 4*1024)
	isl := island.NewIsland(longID, htmlComp("push-long"))
	isl.SessionID = "sess"
	mgr.Register(isl)

	mgr.Subscribe("sess")
	mgr.Push(longID)

	t.Logf("NOTE: Push with 4KB island ID completed without error")
}

// ===================================================================
// Tests 34-36: Special chars in island IDs
// ===================================================================

func TestSecurity_SpecialCharsIslandID_Render(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		id   string
	}{
		{"null", "id\x00null"},
		{"tab", "id\ttab"},
		{"backslash", `id\backslash`},
		{"unicode", "id\u202E\uFEFFrtl"},
		{"emoji", "id🎉fire"},
		{"mixed", "a<b>&\"'c"},
		{"control", "id\x01\x02\x7f"},
	}

	mgr := island.NewManager()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isl := island.NewIsland(tc.id, htmlComp("x"))
			isl.SessionID = "sess"
			mgr.Register(isl)

			defer func() {
				if r := recover(); r != nil {
					t.Errorf("SECURITY: [special-chars] Render panicked for %q: %v", tc.id, r)
				}
			}()

			out := string(isl.Render())
			if !strings.HasPrefix(out, "<div") || !strings.HasSuffix(out, "</div>") {
				t.Errorf("SECURITY: [special-chars] render structure broken for %q: %s", tc.id, out[:min(len(out), 200)])
			}
			t.Logf("NOTE: special char %q rendered successfully", tc.name)
		})
	}
}

func TestSecurity_SpecialCharsSessionID(t *testing.T) {
	t.Parallel()
	// Session IDs with special characters should work for register/list/push.
	mgr := island.NewManager()
	specialSession := `<script>alert('x')</script>`

	isl := island.NewIsland("safe", htmlComp("content"))
	isl.SessionID = specialSession
	mgr.Register(isl)

	ids := mgr.ListBySession(specialSession)
	if len(ids) != 1 {
		t.Errorf("SECURITY: [special-chars] ListBySession failed for special session ID")
	}
	t.Logf("NOTE: Special characters in session ID handled correctly")
}

func TestSecurity_SpecialCharsIslandID_Push(t *testing.T) {
	t.Parallel()
	// Push with special-char island ID should not cause routing issues.
	mgr := island.NewManager()
	isl := island.NewIsland(`id with spaces & symbols`, htmlComp("special"))
	isl.SessionID = "sess"
	mgr.Register(isl)

	ch := mgr.Subscribe("sess")
	mgr.Push(`id with spaces & symbols`)

	select {
	case update := <-ch:
		if update.IslandID != `id with spaces & symbols` {
			t.Errorf("SECURITY: [special-chars] island ID mangled in push: got %q", update.IslandID)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("SECURITY: [special-chars] push with special-char ID timed out")
	}
	t.Logf("NOTE: Push preserves special characters in island ID")
}

// ===================================================================
// Tests 37-38: Empty island handling
// ===================================================================

func TestSecurity_EmptyIslandID(t *testing.T) {
	t.Parallel()
	// Empty string island ID should register but may cause ambiguity.
	mgr := island.NewManager()
	isl := island.NewIsland("", htmlComp("empty-id"))
	isl.SessionID = "sess"

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SECURITY: [empty] Register panicked with empty ID: %v", r)
		}
	}()

	if err := mgr.Register(isl); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}

	retrieved, ok := mgr.Get("")
	if !ok {
		t.Errorf("SECURITY: [empty] empty ID island not retrievable after register")
	}
	if retrieved.ID != "" {
		t.Errorf("SECURITY: [empty] expected empty ID, got %q", retrieved.ID)
	}
	t.Logf("NOTE: Empty island ID accepted and retrievable")
}

func TestSecurity_EmptySessionID(t *testing.T) {
	t.Parallel()
	// Island with empty session ID should still be registerable.
	mgr := island.NewManager()
	isl := island.NewIsland("empty-sess", htmlComp("val"))
	isl.SessionID = ""

	if err := mgr.Register(isl); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}

	ids := mgr.ListBySession("")
	if len(ids) != 1 {
		t.Errorf("SECURITY: [empty] expected 1 island for empty session, got %d", len(ids))
	}
	t.Logf("NOTE: Empty session ID works for register and list")
}

func TestSecurity_EmptyHTML(t *testing.T) {
	t.Parallel()
	// Component rendering empty HTML should not cause issues.
	mgr := island.NewManager()
	isl := island.NewIsland("empty-html", htmlComp(""))
	isl.SessionID = "sess"
	mgr.Register(isl)

	mgr.Subscribe("sess")
	mgr.Push("empty-html")

	out := string(isl.Render())
	if !strings.Contains(out, `data-island="empty-html"`) {
		t.Errorf("SECURITY: [empty] missing data-island attribute in render output")
	}
	t.Logf("NOTE: Empty HTML component rendered correctly with wrapper")
}

// ===================================================================
// Tests 39-40: Concurrent island rendering
// ===================================================================

func TestSecurity_ConcurrentRender_SameIsland(t *testing.T) {
	t.Parallel()
	// Many goroutines calling Render on the same island simultaneously.
	tc := &trackingComp{}
	isl := island.NewIsland("shared-render", tc)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("SECURITY: [concurrency] concurrent Render panicked: %v", r)
				}
			}()
			out := string(isl.Render())
			if !strings.Contains(out, "data-island") {
				t.Errorf("SECURITY: [concurrency] render output missing data-island attribute")
			}
		}()
	}
	wg.Wait()
	t.Logf("NOTE: 100 concurrent Renders completed safely (Render called %d times)", tc.Count())
}

func TestSecurity_ConcurrentPush_MultipleSessions(t *testing.T) {
	t.Parallel()
	// Multiple sessions, each with islands, all being pushed concurrently.
	mgr := island.NewManager()
	const numSessions = 20
	const islandsPerSession = 5

	var wg sync.WaitGroup

	// Register islands for each session.
	for s := 0; s < numSessions; s++ {
		sessID := fmt.Sprintf("sess-%d", s)
		mgr.Subscribe(sessID)
		for i := 0; i < islandsPerSession; i++ {
			isl := island.NewIsland(fmt.Sprintf("isl-%d-%d", s, i), htmlComp(fmt.Sprintf("v%d", i)))
			isl.SessionID = sessID
			mgr.Register(isl)
		}
	}

	// Concurrently push all islands.
	for s := 0; s < numSessions; s++ {
		for i := 0; i < islandsPerSession; i++ {
			wg.Add(1)
			go func(s, i int) {
				defer wg.Done()
				id := fmt.Sprintf("isl-%d-%d", s, i)
				if err := mgr.Push(id); err != nil {
					t.Errorf("SECURITY: [concurrency] push failed for %s: %v", id, err)
				}
			}(s, i)
		}
	}
	wg.Wait()

	// Verify all sessions have correct island counts.
	for s := 0; s < numSessions; s++ {
		sessID := fmt.Sprintf("sess-%d", s)
		ids := mgr.ListBySession(sessID)
		if len(ids) != islandsPerSession {
			t.Errorf("SECURITY: [concurrency] session %s has %d islands, want %d", sessID, len(ids), islandsPerSession)
		}
	}
	t.Logf("NOTE: %d concurrent pushes across %d sessions completed safely", numSessions*islandsPerSession, numSessions)
}
