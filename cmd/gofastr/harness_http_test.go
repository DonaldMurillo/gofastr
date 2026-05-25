package main

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// TestRenderLandingIsGofastrSSR: the sidecar landing page must be
// rendered via core/render + core-ui/html (not a hand-written string
// template). Smoke-test that the rendered HTML contains the expected
// structural tags and that the host the test passes in lands in the
// curl example block.
func TestRenderLandingIsGofastrSSR(t *testing.T) {
	out := string(renderLanding("127.0.0.1:18423"))
	// html.Heading auto-generates an id from the text, so we match
	// the tag opening + content instead of the exact closing tag.
	want := []string{
		"<!doctype html>",
		"<html lang=\"en\">",
		"<title>gofastr harness</title>",
		"<h1",
		">gofastr harness</h1>",
		"<h2",
		">Endpoints</h2>",
		"<ul",
		"/v1/handshake",
		"/v1/ws?session=&lt;id&gt;",   // angle brackets must escape
		"127.0.0.1:18423",             // the host plumbing through
		"class=\"method method-get\"", // SSR-applied CSS classes
		"class=\"method method-post\"",
		"class=\"method method-ws\"",
		"<small>",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("rendered landing missing %q\n--- output (first 800 chars) ---\n%s",
				w, truncateFor(out, 800))
		}
	}
}

// TestRenderLandingEscapesUserContent: the host string is reflected
// into the page; if it ever carried HTML-special chars they must be
// escaped by the SSR primitives (render.Text), not interpreted.
func TestRenderLandingEscapesUserContent(t *testing.T) {
	out := string(renderLanding("evil<script>alert(1)</script>"))
	if strings.Contains(out, "<script>") {
		t.Errorf("unescaped <script> in rendered landing — XSS risk:\n%s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("expected escaped host: %s", truncateFor(out, 800))
	}
}

// TestRenderChatHasInteractiveControls: the chat page must include
// every major feature surface — scrollback, input, sidebar, status
// bar slots, permission modal, embedded auth, SSE wiring.
func TestRenderChatHasInteractiveControls(t *testing.T) {
	out := string(renderChat("127.0.0.1:18424",
		"sess_01HARNESSCHATTESTSESSIONID",
		"eyJhbGciOiJ...test-token"))
	want := []string{
		// Core layout
		"id=\"scrollback\"",
		"id=\"input-form\"",
		"id=\"input\"",
		"id=\"layout\"",
		"id=\"sidebar\"",
		"id=\"main\"",
		// Status bar slots that JS updates live
		"id=\"status-session\"",
		"id=\"status-model\"",
		"id=\"status-cost\"",
		// Permission modal scaffold
		"id=\"permission-modal\"",
		"id=\"perm-yes\"", "id=\"perm-no\"",
		"id=\"perm-tool-btn\"", "id=\"perm-session\"",
		// Sidebar slots
		"id=\"session-list\"",
		"id=\"quick-cmds\"",
		// Embedded auth so the JS can hit /v1/* and SSE without an extra round-trip
		"name=\"harness-session\"", "sess_01HARNESSCHATTESTSESSIONID",
		"name=\"harness-token\"", "eyJhbGciOiJ...test-token",
		// JS wiring
		"new EventSource(",
		"/v1/sessions/",
		"/events?token=",
		"X-Harness-Token",
		"/input",
		"/permission",
		// Styling
		"--accent",
		".bubble.user",
		".bubble.assistant",
		".tool-card",
		".modal-panel",
		".spinner",
		// Endpoints docs page is still linked
		"href=\"/endpoints\"",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("chat page missing %q\n--- first 1.5kb ---\n%s",
				w, truncateFor(out, 1500))
		}
	}
}

// TestChatJSHasWebFeatures: lock in the 10 web-specific affordances
// requested ("all of them"). Each feature has a recognizable
// fingerprint in the served JS/CSS.
func TestChatJSHasWebFeatures(t *testing.T) {
	out := string(renderChat("h", "sess_x", "tok"))
	checks := map[string]string{
		"1. message bubbles":        ".bubble.user",
		"2. session sidebar":        "refreshSessions",
		"3. collapsible tool cards": "tool-card",
		"4. live cost meter":        "bumpCost",
		"5. permission modal":       "openPermissionModal",
		"6. inline diff preview":    "renderDiff",
		"7. markdown rendering":     "renderMarkdown",
		"8. spinner during think":   "className = 'spinner'",
		"9. copy buttons":           "makeCopyBtn",
		"10. history pane":          "session-item",
	}
	for label, fingerprint := range checks {
		if !strings.Contains(out, fingerprint) {
			t.Errorf("feature missing — %s — fingerprint %q not in served HTML",
				label, fingerprint)
		}
	}
}

// TestChatJSRegistersEventListeners: the SSE handler emits named
// events ("event: TextDelta\n..."), so the browser's EventSource
// MUST register a listener for each kind. Plain `onmessage` only
// fires for the default 'message' type and would silently drop
// everything. Regression for the bug where the browser submitted
// input but never showed the response.
func TestChatJSRegistersEventListeners(t *testing.T) {
	out := string(renderChat("h", "sess_x", "tok"))
	// Must explicitly listen for these — anything missing means
	// the browser will swallow that event silently.
	for _, kind := range []string{
		"TextDelta", "ThinkingDelta",
		"ToolCallStarted", "ToolResult",
		"TurnEnded", "PermissionRequested", "Error",
	} {
		if !strings.Contains(out, "'"+kind+"'") {
			t.Errorf("chat JS missing addEventListener for %q", kind)
		}
	}
	if !strings.Contains(out, "addEventListener(k, handleEvent)") {
		t.Errorf("chat JS missing the per-kind addEventListener loop")
	}
}

// TestChatJSSendsContentAsArray: the inline JS must serialize
// SendInput.content as a flat []ContentBlock, NOT wrap it in an
// object like {blocks: [...]}. The server unmarshals into a Go
// slice; the wrapped shape returns HTTP 400 with InvalidBody.
// Regression for the bug surfaced in the first browser test.
func TestChatJSSendsContentAsArray(t *testing.T) {
	out := string(renderChat("h", "sess_x", "tok"))
	if strings.Contains(out, "{ blocks:") || strings.Contains(out, "blocks: [") {
		t.Errorf("chat JS still wraps content in {blocks: ...} — server expects flat array.")
	}
	if !strings.Contains(out, "content: [{ type: 'text'") {
		t.Errorf("chat JS missing the flat [{type: 'text', text: ...}] shape:\n%s",
			truncateFor(out, 1500))
	}
}

// TestRenderChatEscapesSession: the session string is embedded in
// the page; we still want HTML escaping if it somehow contains
// special chars. Belt-and-suspenders since session IDs are
// ULID-shaped, but if a future format ever leaks <>&" it must escape.
func TestRenderChatEscapesSession(t *testing.T) {
	out := string(renderChat("h", "sess<x>", "tok"))
	if strings.Contains(out, "<x>") && !strings.Contains(out, "&lt;x&gt;") {
		t.Errorf("session string not escaped in chat page:\n%s", out)
	}
}

// TestChatJSHasDiffRenderer: the inline JS includes a diff-aware
// renderer for Edit/Write tool results so additions/deletions are
// color-coded inline. Today renderDiff exists but the test for it
// only checks function presence; expand to verify the actual color
// classes are wired.
func TestChatJSHasDiffRendererClasses(t *testing.T) {
	out := string(renderChat("h", "sess_x", "tok"))
	want := []string{
		"renderDiff",
		"diff-add",  // CSS class for + lines
		"diff-del",  // CSS class for - lines
		"diff-hunk", // CSS class for @@ headers
		"detectDiff",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("chat page missing diff renderer piece %q", w)
		}
	}
}

// TestChatHasAllowAlwaysButton: the permission modal must offer
// "Allow always" so the user can persist the rule and stop seeing
// the prompt across runs. Pinned with both the SSR scaffold and the
// JS handler that posts scope=always.
func TestChatHasAllowAlwaysButton(t *testing.T) {
	out := string(renderChat("h", "sess_x", "tok"))
	want := []string{
		`id="perm-always"`,
		`>Allow always</button>`,
		`answerPermission('allow', 'always')`,
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("permission modal missing %q\n--- first 1.5kb ---\n%s",
				w, truncateFor(out, 1500))
		}
	}
}

// TestChatHasTaskPanel TDD: the web client should render a task
// panel populated by polling /v1/sessions/<id>/tasks. The panel
// has a known container id and the JS that polls it.
func TestChatHasTaskPanel(t *testing.T) {
	out := string(renderChat("h", "sess_x", "tok"))
	want := []string{
		"id=\"task-panel\"",            // SSR scaffold
		"refreshTasks",                  // polling fn
		"/v1/sessions/' + SESSION + '/tasks", // endpoint
		"data-status=\"in_progress\"",   // status-driven styling hook
		"data-status=\"completed\"",
		"data-status=\"pending\"",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("chat page missing task-panel piece %q\n--- first 1.5kb ---\n%s",
				w, truncateFor(out, 1500))
		}
	}
}

// TestChatJSDoesNotOverEscape catches the bug where backslashes
// inside the Go raw-string `const chatJS = ` got literally doubled
// because Go raw strings pass `\\` through as two chars. The browser
// then sees `/\\s\\S/` in a regex literal, which matches a literal
// backslash followed by s/S — not whitespace. Result: every regex
// (markdown, diff parser, line splits) silently fails and the chat
// UI looks dead.
//
// The fingerprints below are the common mistakes: \\s, \\n, \\*, \\d
// inside the served JS. None of them are legitimate in a JS regex
// LITERAL (between /.../). They WOULD be valid inside a JS string
// like 'a\\nb', so we restrict the search to lines that contain a
// regex literal or a split('\\n') / replace('\\n', ...) pattern.
func TestChatJSDoesNotOverEscape(t *testing.T) {
	out := string(renderChat("h", "sess_x", "tok"))
	// Surgical: any line that has BOTH a JS regex slash and a \\X
	// inside it is almost certainly the bug.
	badInRegex := regexp.MustCompile(`/[^/\n]*\\\\[snStdwbW*+.\\][^/\n]*/`)
	if loc := badInRegex.FindStringIndex(out); loc != nil {
		// Show 80 chars of context.
		start := loc[0] - 30
		if start < 0 {
			start = 0
		}
		end := loc[1] + 30
		if end > len(out) {
			end = len(out)
		}
		t.Errorf("over-escaped regex literal in served JS — Go raw strings pass `\\\\` through as two chars; use single `\\` for JS regexes.\nContext: …%s…",
			out[start:end])
	}
	// String-form: split('\\n') / split('\\r') etc. — these turn into
	// literal "\n" two-char delimiters in JS, not newline splits.
	badInString := regexp.MustCompile(`split\(\s*'\\\\[nrt]\s*'\s*\)`)
	if loc := badInString.FindStringIndex(out); loc != nil {
		start := loc[0]
		end := loc[1]
		t.Errorf("over-escaped string literal in served JS: %s — should be a single backslash so the JS escape `\\n` parses to a newline.",
			out[start:end])
	}
}

// TestChatJSParsesAsValidJS runs node on the served JS body to catch
// syntax errors before they reach the browser. Skipped if node isn't
// installed locally — the string-pattern checks above provide the
// always-on guard.
func TestChatJSParsesAsValidJS(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available; relying on TestChatJSDoesNotOverEscape")
	}
	// The serving page wraps the JS in an IIFE that touches document/
	// EventSource/etc. Node doesn't have those globals — but it can
	// still PARSE the file for syntax errors. We use `--check` to
	// validate without executing.
	cmd := exec.Command("node", "--check", "-")
	cmd.Stdin = strings.NewReader(chatJS)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("chatJS failed node --check syntax validation:\n%s", string(out))
	}
}

func truncateFor(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
