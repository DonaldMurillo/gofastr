package pluginhost

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// The frame-side channel client lives in frame/frameclient.js and mirrors
// the host broker (host/pluginhost.js). Like the broker, its invariants
// can't be Go-typed away, so the dangerous regressions are pinned at the
// source level (deterministic, no browser), and the channel contract BOTH
// files must share is pinned symmetrically.

// (1) postMessage source validation, mirror of the broker: messages are
// accepted ONLY from the parent window, the envelope version is checked,
// and only src:"host" traffic is dispatched.
func TestFrameClientJS_ValidatesMessageSource(t *testing.T) {
	js := string(frameClientJSBytes)
	// Scan CODE lines only: the header comment states the invariant in the
	// same words, so a whole-file Contains would stay green with the actual
	// check deleted (caught by mutation during review).
	code := nonCommentJS(js)
	if !strings.Contains(code, "event.source === window.parent") &&
		!strings.Contains(code, "window.parent === event.source") {
		t.Error("onMessage must accept only messages whose source is the parent window (in code, not prose)")
	}
	if !strings.Contains(code, "msg.v !== ENVELOPE_VERSION") {
		t.Error("onMessage must reject a wrong envelope version")
	}
	if !strings.Contains(code, `msg.src !== "host"`) {
		t.Error("onMessage must reject messages not marked src:host")
	}
}

// nonCommentJS strips whole-line and trailing // comments, returning the
// executable lines joined — the same approach as the NoExternalURLs scan,
// shared so security pins can't be satisfied by prose.
func nonCommentJS(js string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(js, "\n") {
		code := strings.TrimSpace(line)
		if strings.HasPrefix(code, "//") || strings.HasPrefix(code, "*") ||
			strings.HasPrefix(code, "/*") {
			continue
		}
		if idx := strings.Index(code, "//"); idx >= 0 {
			code = strings.TrimSpace(code[:idx])
		}
		b.WriteString(code)
		b.WriteString("\n")
	}
	return b.String()
}

// (2) The frame→host post uses targetOrigin "*" (the frame is opaque, a
// concrete targetOrigin is the wrong tool); the source check, not an
// origin string, is the gate. Mirror of the broker pin so nobody
// "hardens" it into an origin that silently drops every message.
func TestFrameClientJS_PostsWithWildcardTargetOrigin(t *testing.T) {
	js := string(frameClientJSBytes)
	if !strings.Contains(js, `postMessage(env, "*")`) {
		t.Error("postToHost must use targetOrigin \"*\" (source check is the gate)")
	}
}

// (3) No external URLs: the client runs inside the sandboxed frame with
// connect-src 'none' as the exfiltration guard, and a script that phones
// home would defeat the point of the isolation. Scan non-comment lines
// (same comment-stripping approach as TestBrokerJS_NeverEmitsAllowSameOrigin).
func TestFrameClientJS_NoExternalURLs(t *testing.T) {
	js := string(frameClientJSBytes)
	for line := range strings.SplitSeq(js, "\n") {
		code := strings.TrimSpace(line)
		if strings.HasPrefix(code, "//") {
			continue // whole-line comment
		}
		if idx := strings.Index(code, "//"); idx >= 0 {
			code = strings.TrimSpace(code[:idx]) // strip trailing comment
		}
		if strings.Contains(code, "http://") || strings.Contains(code, "https://") {
			t.Errorf("external URL in executable code: %q", strings.TrimSpace(line))
		}
	}
}

// The envelope version is a wire contract between the two scripts: if they
// drift, every message is silently dropped by the version check on both
// sides. Pin them equal.
func TestEnvelopeVersionsAgree(t *testing.T) {
	re := regexp.MustCompile(`ENVELOPE_VERSION = (\d+)`)
	broker := re.FindStringSubmatch(string(brokerJSBytes))
	client := re.FindStringSubmatch(string(frameClientJSBytes))
	if broker == nil {
		t.Fatal("brokerJSBytes: ENVELOPE_VERSION declaration not found")
	}
	if client == nil {
		t.Fatal("frameClientJSBytes: ENVELOPE_VERSION declaration not found")
	}
	if broker[1] != client[1] {
		t.Errorf("envelope versions disagree: broker v%s, frame client v%s", broker[1], client[1])
	}
}

// The bidirectional channel contract is shared by both files: bounded
// in-flight map, the four error codes, and handler registration. Pin the
// literals (quoted forms — comments mention the codes unquoted, so a pin
// only passes on executable code).
func TestChannelContractPins(t *testing.T) {
	broker := string(brokerJSBytes)
	client := string(frameClientJSBytes)
	for _, s := range []string{
		"MAX_INFLIGHT = 64",
		`"E_SATURATED"`,
		`"E_TEARDOWN"`,
		`"E_NO_HANDLER"`,
		`"E_HANDLER"`,
	} {
		if !strings.Contains(broker, s) {
			t.Errorf("broker missing channel contract literal %s", s)
		}
		if !strings.Contains(client, s) {
			t.Errorf("frame client missing channel contract literal %s", s)
		}
	}
	if !strings.Contains(broker, "onRequest") {
		t.Error("broker must expose onRequest handler registration")
	}

	// The bound must be ENFORCED, not just declared: pin the saturation
	// checks that reject before posting.
	if !strings.Contains(broker, "Object.keys(st.pending).length >= MAX_INFLIGHT") {
		t.Error("broker request() must reject when the pending map is full")
	}
	if !strings.Contains(client, "Object.keys(pending).length >= MAX_INFLIGHT") {
		t.Error("frame client sendRequest() must reject when the pending map is full")
	}

	// cleanup() must REJECT pending entries with E_TEARDOWN, not just clear
	// their timers — otherwise teardown leaks unresolved promises forever.
	ci := strings.Index(broker, "function cleanup(")
	if ci < 0 {
		t.Fatal("broker: cleanup() not found")
	}
	end := strings.Index(broker[ci:], "\n  }")
	if end < 0 {
		t.Fatal("broker: cleanup() end not found")
	}
	if !strings.Contains(broker[ci:ci+end], `"E_TEARDOWN"`) {
		t.Error("broker cleanup() must reject pending requests with E_TEARDOWN before clearing")
	}
}

// RegisterFrameClientRoute is idempotent: many plugins may call it from
// Init, but only the first registration lands (a duplicate router pattern
// panics). Mirror of TestRegisterBrokerRoute_Idempotent.
func TestRegisterFrameClientRoute_Idempotent(t *testing.T) {
	rt := router.New()
	RegisterFrameClientRoute(rt)
	RegisterFrameClientRoute(rt) // must NOT panic
	RegisterFrameClientRoute(rt)

	n := 0
	for _, rr := range rt.Routes() {
		if rr.Method == FrameClientRouteMethod && rr.Pattern == FrameClientScriptURL {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("frame client route registered %d times, want exactly 1", n)
	}
}

// The frame client route is a FRAMED asset, unlike the broker route: it is
// fetched by opaque-origin frame documents, so it must carry the CORP
// cross-origin relaxation (the global same-origin default would block the
// frame's script fetch), plus the nosniff every asset carries.
func TestFrameClientRouteServedFramed(t *testing.T) {
	rt := router.New()
	RegisterFrameClientRoute(rt)

	srv := httptest.NewServer(rt)
	defer srv.Close()
	resp, err := http.Get(srv.URL + FrameClientScriptURL)
	if err != nil {
		t.Fatalf("GET frame client: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("frame client status=%d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("frame client Content-Type=%q", ct)
	}
	if got := resp.Header.Get("Cross-Origin-Resource-Policy"); got != "cross-origin" {
		t.Errorf("frame client CORP=%q, want cross-origin (opaque-origin fetcher)", got)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("frame client must carry nosniff")
	}
	if len(body) == 0 {
		t.Error("frame client body empty")
	}
}
