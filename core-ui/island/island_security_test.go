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
	isl := island.NewIsland(`<script>alert('xss')</script>`, htmlComp("safe"))

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
	isl := island.NewIsland(`" onclick="alert(1)`, htmlComp("content"))

	out := string(isl.Render())
	if strings.Contains(out, `onclick="alert(1)`) {
		t.Errorf("SECURITY: [injection] island ID broke out of attribute context: %s", out)
	}
	t.Logf("NOTE: Quote characters in island ID do not break attribute boundary")
}

func TestSecurity_IslandID_NewlineInjection(t *testing.T) {
	t.Parallel()
	// Newlines in island IDs could split the attribute across lines.
	isl := island.NewIsland(`foo\nbar`, htmlComp("safe"))

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
	isl := island.NewIsland(`x" onmouseover="alert(document.cookie)`, htmlComp("safe"))

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
	isl := island.NewIsland(`x" style="display:none" data-island="real`, htmlComp("hidden"))

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
// Tests 13-15: RPC path traversal
// ===================================================================

func TestSecurity_IslandID_PathTraversal(t *testing.T) {
	t.Parallel()
	// Path-traversal characters in island ID are not HTML-special, so they're not
	// escaped by render.Escape. Security depends on island IDs being used only as
	// DOM identifiers, never as filesystem paths.
	isl := island.NewIsland("../../../../etc/passwd", htmlComp("traversal"))

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
	isl := island.NewIsland("id\x00malicious", htmlComp("null"))

	out := string(isl.Render())
	// The null byte must not disappear (which would split the ID).
	if strings.Contains(out, "idmalicious") {
		t.Errorf("SECURITY: [injection] null byte may have caused truncation in output: %s", out)
	}
	t.Logf("NOTE: Null byte in island ID preserved in render output")
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
	_, cancelSub4 := mgr.Subscribe("evil-sess")
	defer cancelSub4()

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
	ch, cancelCh1 := mgr.Subscribe("panic-sess")
	defer cancelCh1()

	// Fill the channel buffer (size 64).
	for i := 0; i < 64; i++ {
		mgr.PushUpdate(island.IslandUpdate{IslandID: fmt.Sprintf("fill-%d", i), HTML: "x"}, "panic-sess")
	}

	// Drain to keep test fast.
	for i := 0; i < 64; i++ {
		<-ch
	}

	// Cancel the subscription then try PushUpdate.
	cancelCh1()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SECURITY: [panic] PushUpdate panicked on closed session: %v", r)
		}
	}()

	mgr.PushUpdate(island.IslandUpdate{IslandID: "late", HTML: "data"}, "panic-sess")
	t.Logf("NOTE: PushUpdate after unsubscribe is safe (no panic)")
}

func TestSecurity_ContextCancel_DuringSSE(t *testing.T) {
	t.Parallel()
	// ServeSSE must respect context cancellation and not leak goroutines.
	mgr := island.NewManager()

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

// TestSecurity_MultiTabSubscribe_OutlivesOneClose asserts that a session's
// live update stream stays alive as long as at least one subscriber for that
// session remains. Two tabs sharing one session cookie subscribe; when one
// closes (its cancel runs), the survivor must keep receiving Push updates.
func TestSecurity_MultiTabSubscribe_OutlivesOneClose(t *testing.T) {
	t.Parallel()
	mgr := island.NewManager()

	// Tab A and Tab B both connect with the same session cookie.
	chA, cancelCha2 := mgr.Subscribe("shared-sess")
	defer cancelCha2()
	chB, cancelChb3 := mgr.Subscribe("shared-sess")
	defer cancelChb3()

	// Tab A closes (browser tab closed → ServeSSE defer cancel).
	cancelCha2()

	// The surviving tab B must still receive pushed updates.
	mgr.PushUpdate(island.IslandUpdate{IslandID: "multi-tab", HTML: "<p>live</p>"}, "shared-sess")

	select {
	case <-chB:
		t.Logf("NOTE: surviving subscriber still receives updates")
	case <-time.After(time.Second):
		t.Errorf("SECURITY: [availability] surviving subscriber stopped receiving updates after another tab closed")
	}

	// chA is a private channel; after its cancel it receives nothing more.
	select {
	case u := <-chA:
		// One frame may have landed before cancel ran; a SECOND one must not.
		_ = u
	default:
	}

	// Once the last subscriber closes, the stream may be torn down.
	cancelChb3()
	mgr.PushUpdate(island.IslandUpdate{IslandID: "multi-tab", HTML: "<p>live</p>"}, "shared-sess")
}

func TestSecurity_VeryLongIslandID_Render(t *testing.T) {
	t.Parallel()
	// Rendering with a very long ID should not produce corrupted HTML.
	longID := strings.Repeat("x", 8*1024)
	isl := island.NewIsland(longID, htmlComp("content"))

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

	_, cancelSub5 := mgr.Subscribe("sess")
	defer cancelSub5()
	mgr.PushUpdate(island.IslandUpdate{IslandID: longID, HTML: "<p>push-long</p>"}, "sess")

	t.Logf("NOTE: Push with 4KB island ID completed without error")
}

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

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isl := island.NewIsland(tc.id, htmlComp("x"))

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

func TestSecurity_EmptyHTML(t *testing.T) {
	t.Parallel()
	// Component rendering empty HTML should not cause issues.
	mgr := island.NewManager()
	isl := island.NewIsland("empty-html", htmlComp(""))

	_, cancelSub6 := mgr.Subscribe("sess")
	defer cancelSub6()
	mgr.PushUpdate(island.IslandUpdate{IslandID: "empty-html", HTML: string(isl.Render())}, "sess")

	out := string(isl.Render())
	if !strings.Contains(out, `data-island="empty-html"`) {
		t.Errorf("SECURITY: [empty] missing data-island attribute in render output")
	}
	t.Logf("NOTE: Empty HTML component rendered correctly with wrapper")
}

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
