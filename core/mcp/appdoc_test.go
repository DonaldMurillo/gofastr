package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// ── Structure ───────────────────────────────────────────────────────

// The assembled document carries exactly the boilerplate the author
// should never retype, and nothing else: doctype, charset/viewport, title,
// one root element wrapping the body, the widget client <script src>, and
// the author's script after it in execution order. No CSS anywhere —
// the builder emits structure only, hard rule 7's widget-side twin.
func TestWidgetDocument_AssemblesDocument(t *testing.T) {
	doc := WidgetDocument{
		Title:  "Studio",
		Body:   `<p id="status">idle</p>`,
		Script: `var app = window.__gofastrMcpApp; app.connect({});`,
	}
	html, err := doc.HTML()
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"<!DOCTYPE html>",
		`<html lang="en">`,
		`<meta charset="utf-8">`,
		`<meta name="viewport" content="width=device-width, initial-scale=1">`,
		`<title>Studio</title>`,
		`<div id="app"><p id="status">idle</p></div>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("document missing %q:\n%s", want, html)
		}
	}

	// The client script tag comes from the constant the server mounts, and
	// it precedes the author's script so window.__gofastrMcpApp exists
	// when the author's code runs.
	clientTag := `<script src="` + WidgetClientScriptURL + `"></script>`
	if !strings.Contains(html, clientTag) {
		t.Errorf("document missing the widget client script tag %q:\n%s", clientTag, html)
	}
	if ci, ai := strings.Index(html, clientTag), strings.Index(html, "window.__gofastrMcpApp"); ci < 0 || ai < ci {
		t.Errorf("author script must come after the client script tag")
	}
	if strings.Contains(html, "style") {
		t.Errorf("builder must emit zero styling; found a style reference:\n%s", html)
	}
}

// Zero fields keep usable defaults (lang, root id) and an empty <title>
// element rather than an omitted one, so the document stays valid HTML5
// (a missing title is a validator error) with no required configuration.
func TestWidgetDocument_Defaults(t *testing.T) {
	html, err := (WidgetDocument{Body: "<p>x</p>"}).HTML()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `<html lang="en">`) {
		t.Errorf("default Lang = %q, want en", "missing")
	}
	if !strings.Contains(html, `<div id="app">`) {
		t.Error("default RootID missing, want id=\"app\"")
	}
	if !strings.Contains(html, "<title></title>") {
		t.Error("empty Title must still emit a <title> element")
	}
}

// Same input, same bytes: AppConfig.HTML is served verbatim per read, so
// a builder that varied between calls would ship different widgets per
// resources/read.
func TestWidgetDocument_Deterministic(t *testing.T) {
	doc := WidgetDocument{Title: "T", Body: "<p>b</p>", Script: "var x = 1;"}
	a, err := doc.HTML()
	if err != nil {
		t.Fatal(err)
	}
	b, err := doc.HTML()
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Error("HTML() must be deterministic")
	}
}

// ── Validation ──────────────────────────────────────────────────────

func TestWidgetDocument_Validation(t *testing.T) {
	cases := []struct {
		name string
		doc  WidgetDocument
		want string
	}{
		{"empty body and script", WidgetDocument{}, "Body, a Script"},
		{"whitespace-only body and script", WidgetDocument{Body: "  \t ", Script: "  "}, "Body, a Script"},
		{"script close tag", WidgetDocument{Body: "x", Script: `el.innerHTML = "</script><script>alert(1)</script>";`}, `"</script"`},
		{"script close tag upper case", WidgetDocument{Body: "x", Script: `el.innerHTML = "</SCRIPT>";`}, `"</SCRIPT"`},
		{"script close tag mixed case", WidgetDocument{Body: "x", Script: `el.innerHTML = "</ScRiPt>";`}, `"</ScRiPt"`},
		{"script html comment open", WidgetDocument{Body: "x", Script: `var s = "<!--";`}, `"<!--"`},
	}
	for _, tc := range cases {
		_, err := tc.doc.HTML()
		if err == nil {
			t.Errorf("%s: expected an error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not name %q", tc.name, err, tc.want)
		}
	}

	// The documented workarounds pass: the author writes the sequence
	// escaped inside a string literal.
	if _, err := (WidgetDocument{Body: "x", Script: `el.innerHTML = "<\/script>ok";`}).HTML(); err != nil {
		t.Errorf("escaped sequence in a string literal should pass, got %v", err)
	}
}

// ── Escaping: the breakout test ─────────────────────────────────────

// scriptSrcRE captures the src of every script tag that carries one. The
// round-trip test compares the captured value for EQUALITY against
// WidgetClientScriptURL, because a substring pin would pass against a
// longer URL that merely contains the constant.
var scriptSrcRE = regexp.MustCompile(`<script\b[^>]*\bsrc="([^"]*)"`)

// A hostile Title and RootID cannot break out of their contexts. Title is
// HTML-text-escaped and RootID attribute-escaped by html/template, so the
// classic payload — close the element, open a new script — survives only
// as inert text. The proof: the document contains exactly the two script
// elements the builder itself emits (client + author), never a third,
// and the payload never appears as markup.
func TestWidgetDocument_EscapesDataFields(t *testing.T) {
	doc := WidgetDocument{
		Title:  `</title></head><body><script>alert("title")</script>`,
		RootID: `app" onload="alert(1)`,
		Body:   "<p>b</p>",
		Script: "var x = 1;",
	}
	html, err := doc.HTML()
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(html, `<script>alert("title")</script>`) {
		t.Errorf("Title payload reached the document as markup:\n%s", html)
	}
	if strings.Contains(html, `id="app" onload=`) || strings.Contains(html, `onload="alert`) {
		t.Errorf("RootID payload broke out of the attribute:\n%s", html)
	}
	// The escaped title keeps its text: readable, inert.
	if !strings.Contains(html, "&lt;script&gt;alert(&#34;title&#34;)&lt;/script&gt;") &&
		!strings.Contains(html, "&lt;script&gt;alert(&quot;title&quot;)&lt;/script&gt;") {
		t.Errorf("Title must survive as escaped text, got:\n%s", html)
	}

	// Structure is intact: exactly two <script> elements, and the root
	// div's id attribute is one attribute.
	if n := strings.Count(html, "<script"); n != 2 {
		t.Errorf("hostile data fields must not add script elements: found %d in:\n%s", n, html)
	}
	srcs := scriptSrcRE.FindAllStringSubmatch(html, -1)
	if len(srcs) != 1 {
		t.Fatalf("exactly one script may carry a src, found %d in:\n%s", len(srcs), html)
	}
}

// ── The round trip ──────────────────────────────────────────────────

// Register an app whose HTML comes from the builder, read the ui://
// resource back over resources/read, and assert the document the WIRE
// serves references exactly the script URL the server mounts — the drift
// this test exists to catch is a one-character typo in the baked-in URL,
// which in production is a widget that renders and silently never
// receives anything. Then call the linking tool, because a ui:// resource
// no tool points at is unreachable for the model.
func TestWidgetDocument_RoundTrip(t *testing.T) {
	doc := WidgetDocument{
		Title:  "Studio",
		Body:   `<p id="status">idle</p>`,
		Script: `var app = window.__gofastrMcpApp; app.connect({ availableDisplayModes: ["inline"] });`,
	}
	html, err := doc.HTML()
	if err != nil {
		t.Fatal(err)
	}

	s := NewServer()
	if err := s.RegisterApp(AppConfig{
		Name:        "studio",
		Description: "Open the studio widget.",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			return fmt.Sprintf("studio:%v", args["q"]), nil
		},
		ResourceURI: "ui://demo/studio.html",
		HTML:        html,
	}); err != nil {
		t.Fatal(err)
	}

	// resources/read returns the built document verbatim.
	p, _ := json.Marshal(map[string]any{"uri": "ui://demo/studio.html"})
	resp := s.HandleRequest(t.Context(), Request{JSONRPC: "2.0", ID: 1, Method: "resources/read", Params: p})
	if resp.Error != nil {
		t.Fatalf("resources/read: %v", resp.Error)
	}
	text, ok := wireResult(t, resp)["contents"].([]any)[0].(map[string]any)["text"].(string)
	if !ok {
		t.Fatalf("resource text missing: %v", resp.Result)
	}

	// The document's one src-bearing script tag loads EXACTLY the URL the
	// server mounts the client at. Equality, not substring: a drifted URL
	// that merely contains the constant must fail here.
	srcs := scriptSrcRE.FindAllStringSubmatch(text, -1)
	if len(srcs) != 1 {
		t.Fatalf("built document must carry exactly one script src, found %d:\n%s", len(srcs), text)
	}
	if srcs[0][1] != WidgetClientScriptURL {
		t.Errorf("document script src = %q, want exactly %q (the URL the server mounts)", srcs[0][1], WidgetClientScriptURL)
	}

	// The linking tool answers, so the model can reach the widget.
	call := callTool(t, s, "studio", map[string]any{"q": "hello"})
	if call.Error != nil {
		t.Fatalf("tools/call studio: %v", call.Error)
	}
}
