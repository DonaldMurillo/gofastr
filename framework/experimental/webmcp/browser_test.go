package webmcp

// Browser end-to-end proof for the WebMCP bridge: a real Chromium
// launched with --enable-blink-features=WebMCP (the flag that exposes
// navigator.modelContext without waiting for the origin trial), loading
// a page whose only script tag is the mounted bridge, then registering
// and executing tools through the actual browser API.
//
// Skips (loudly) in -short mode, when no Chromium-family browser is
// installed, and when the installed browser predates WebMCP (needs
// Chromium 146+).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/internal/browserpath"
)

// webmcpServer mounts a Host with four tools on a real router and
// records what the endpoints observed.
type webmcpServer struct {
	srv *httptest.Server

	mu           sync.Mutex
	echoBody     string
	echoHdr      http.Header
	searchQ      string
	searchSource string
}

func newWebmcpServer(t *testing.T) *webmcpServer {
	t.Helper()
	ws := &webmcpServer{}
	rt := router.New()

	rt.Post("/api/echo", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		ws.mu.Lock()
		ws.echoBody = string(b)
		ws.echoHdr = r.Header.Clone()
		ws.mu.Unlock()
		var in struct {
			Msg string `json:"msg"`
		}
		_ = json.Unmarshal(b, &in)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"msg": strings.ToUpper(in.Msg)})
	}))
	rt.Get("/api/search", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws.mu.Lock()
		ws.searchQ = r.URL.Query().Get("q")
		ws.searchSource = r.URL.Query().Get("source")
		ws.mu.Unlock()
		fmt.Fprintf(w, `{"hits":["%s-1"]}`, r.URL.Query().Get("q"))
	}))
	rt.Post("/api/broken", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))

	h := New(WithInstructions("Probe the scene before mutating it."))
	h.Group("scene", WithDescription("Scene tools."))
	for _, tl := range []Tool{
		{
			Name:        "echo_upper",
			Description: "Uppercases msg.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}},"required":["msg"]}`),
			Method:      http.MethodPost,
			Path:        "/api/echo",
		},
		{
			Name:        "search",
			Description: "Searches by q.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`),
			Method:      http.MethodGet,
			// The baked-in query param collides with an input key on
			// purpose: the bridge must let the input override it.
			Path: "/api/search?source=baked",
			// Registration succeeding with annotations set proves the
			// browser accepts the forwarded annotations object.
			ReadOnlyHint:         true,
			UntrustedContentHint: true,
		},
		{
			Name:        "broken",
			Description: "Always fails server-side.",
			Method:      http.MethodPost,
			Path:        "/api/broken",
		},
		{
			// Regression: a schema with an own "__proto__" key must not
			// break the bridge (it embeds via JSON.parse, not an object
			// literal, where "__proto__" is a prototype setter).
			Name:        "proto_probe",
			Description: "Schema stress: own __proto__ property.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"__proto__":{"type":"string"}}}`),
			Method:      http.MethodPost,
			Path:        "/api/echo",
		},
		{
			// Richer metadata (group, examples, output schema) must not
			// break registration: the browser proposal cannot forward
			// these fields, and the bridge must degrade safely — ignore
			// what it cannot pass on, register what it can.
			Name:        "grouped_probe",
			Group:       "scene",
			Description: "Schema stress: group, examples, output schema.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}},"required":["msg"]}`),
			Examples: []Example{{
				Summary: "Shout a message",
				Input:   json.RawMessage(`{"msg":"hello"}`),
			}},
			OutputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`),
			Method:       http.MethodPost,
			Path:         "/api/echo",
		},
	} {
		if err := h.Register(tl); err != nil {
			t.Fatalf("register %s: %v", tl.Name, err)
		}
	}
	scriptURL, err := h.Mount(rt, nil, WithBridgeDebug())
	if err != nil {
		t.Fatalf("mount: %v", err)
	}

	rt.Get("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "user-42", Path: "/"})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The CSRF middleware minted a token on this GET; expose it the
		// way the app shell does, via the meta tag the bridge reads.
		fmt.Fprintf(w, `<!doctype html><html><head><title>webmcp e2e</title>`+
			`<meta name="csrf-token" content=%q>`+
			`<script defer src=%q></script></head><body><h1>webmcp</h1></body></html>`,
			middleware.TokenFromContext(r.Context()), scriptURL)
	}))

	// The real double-submit CSRF middleware wraps everything: the
	handler := middleware.CSRF(middleware.CSRFConfig{
		SecretKey: []byte("webmcp-e2e-test-key"),
	})(rt)
	ws.srv = httptest.NewServer(handler)
	t.Cleanup(ws.srv.Close)
	return ws
}

func webmcpBrowserCtx(t *testing.T) context.Context {
	t.Helper()
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	execPath, ok := browserpath.Find()
	if !ok {
		t.Skip("browser E2E requires Chrome, Chromium, or Edge")
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(execPath),
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		// The whole point: WebMCP is a flagged experimental web platform
		// feature; this exposes navigator.modelContext on Chromium 146+.
		chromedp.Flag("enable-blink-features", "WebMCP"),
		chromedp.WSURLReadTimeout(90*time.Second),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	ctx, cancel := chromedp.NewContext(allocCtx)
	t.Cleanup(cancel)
	ctx, tcancel := context.WithTimeout(ctx, 90*time.Second)
	t.Cleanup(tcancel)
	return ctx
}

// waitForTools polls getTools until the sorted name list matches want.
// The bridge script is deferred and registration is async, so a fixed
// wait would flake.
func waitForTools(t *testing.T, ctx context.Context, want string) {
	t.Helper()
	var names string
	deadline := time.Now().Add(15 * time.Second)
	for {
		if err := evalAwait(ctx,
			`(document.modelContext || navigator.modelContext).getTools().then(ts => ts.map(t => t.name).sort().join(","))`,
			&names); err != nil {
			t.Fatalf("getTools: %v", err)
		}
		if names == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("tools never registered; getTools() = %q, want %q", names, want)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// evalAwait evaluates a promise-returning expression and decodes its
// JSON-string resolution into out.
func evalAwait(ctx context.Context, expr string, out *string) error {
	return chromedp.Run(ctx, chromedp.Evaluate(expr, out,
		func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}))
}

func TestBridgeRegistersAndExecutesTools(t *testing.T) {
	ws := newWebmcpServer(t)
	ctx := webmcpBrowserCtx(t)

	if err := chromedp.Run(ctx, chromedp.Navigate(ws.srv.URL+"/")); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	var has bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`("modelContext" in document) || ("modelContext" in navigator)`, &has)); err != nil {
		t.Fatalf("feature probe: %v", err)
	}
	if !has {
		t.Skip("modelContext absent even with --enable-blink-features=WebMCP; browser predates WebMCP (needs Chromium 146+)")
	}

	waitForTools(t, ctx, "broken,echo_upper,get_app_instructions,grouped_probe,proto_probe,search")

	// The opt-in debug state is bounded and truthful: every tool
	// attempted, every tool registered, nothing failed, no invocation
	// yet.
	var dbg string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`JSON.stringify(window.__gofastrWebMCP)`, &dbg)); err != nil {
		t.Fatalf("debug state: %v", err)
	}
	if want := `{"supported":true,"attempted":6,"registered":6,"failed":[],"lastStatus":""}`; dbg != want {
		t.Fatalf("debug state = %s, want %s", dbg, want)
	}

	// The generated orientation tool registers like any other (it
	// skips Register, so its declaration must be browser-complete on
	// its own) and executes against the instructions route.
	var instr string
	if err := evalAwait(ctx, execToolExpr(InstructionsToolName, `{}`), &instr); err != nil {
		t.Fatalf("execute %s: %v", InstructionsToolName, err)
	}
	assertToolResult(t, instr, false, `{"instructions":"Probe the scene before mutating it."}`)

	// POST tool: input arrives as a JSON body, session cookie and the
	// WebMCP marker header ride along, result text is the endpoint body.
	var res string
	if err := evalAwait(ctx, execToolExpr("echo_upper", `{msg:"hi"}`), &res); err != nil {
		t.Fatalf("execute echo_upper: %v", err)
	}
	assertToolResult(t, res, false, `{"msg":"HI"}`)
	ws.mu.Lock()
	if ws.echoBody != `{"msg":"hi"}` {
		t.Fatalf("server saw body %q", ws.echoBody)
	}
	if c := ws.echoHdr.Get("Cookie"); !strings.Contains(c, "session=user-42") {
		t.Fatalf("session cookie missing; Cookie: %q", c)
	}
	if ws.echoHdr.Get("X-Gofastr-WebMCP") != "1" {
		t.Fatal("X-Gofastr-WebMCP marker header missing")
	}
	// Reaching the handler at all proves the double-submit check
	// passed; assert the header was the reason, not a middleware hole.
	if ws.echoHdr.Get("X-CSRF-Token") == "" {
		t.Fatal("bridge sent no X-CSRF-Token; the CSRF middleware should have 403'd")
	}
	ws.mu.Unlock()

	// GET tool: input folds into the query string, and an input key
	// overrides the path's baked-in param of the same name.
	if err := evalAwait(ctx, execToolExpr("search", `{q:"gopher", source:"agent"}`), &res); err != nil {
		t.Fatalf("execute search: %v", err)
	}
	assertToolResult(t, res, false, `{"hits":["gopher-1"]}`)
	ws.mu.Lock()
	if ws.searchQ != "gopher" {
		t.Fatalf("server saw q=%q", ws.searchQ)
	}
	if ws.searchSource != "agent" {
		t.Fatalf("input did not override the baked-in query param; server saw source=%q", ws.searchSource)
	}
	ws.mu.Unlock()

	// A null input value is skipped, not stringified: the baked-in
	// param survives instead of becoming the literal "null".
	if err := evalAwait(ctx, execToolExpr("search", `{q:"nulltest", source:null}`), &res); err != nil {
		t.Fatalf("execute search with null: %v", err)
	}
	assertToolResult(t, res, false, `{"hits":["nulltest-1"]}`)
	ws.mu.Lock()
	if ws.searchSource != "baked" {
		t.Fatalf("null input value was not skipped; server saw source=%q", ws.searchSource)
	}
	ws.mu.Unlock()

	// Non-2xx endpoint: the agent sees isError, not a throw.
	if err := evalAwait(ctx, execToolExpr("broken", `{}`), &res); err != nil {
		t.Fatalf("execute broken: %v", err)
	}
	assertToolResult(t, res, true, "boom")

	// The debug state's last invocation status reflects the failing
	// call without echoing any input.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`window.__gofastrWebMCP.lastStatus`, &dbg)); err != nil {
		t.Fatalf("debug lastStatus: %v", err)
	}
	if dbg != "http_500" {
		t.Fatalf("debug lastStatus = %q, want http_500", dbg)
	}
}

// TestBridgeReadsCSRFMetaAtDispatchTime proves the CSRF token is read
// when a tool executes, not when the bridge script parses: removing the
// meta tag makes the next unsafe call 403 (isError), and restoring it
// makes the call succeed again. A parse-time read would keep succeeding
// after removal and a missing-meta page would never recover.
func TestBridgeReadsCSRFMetaAtDispatchTime(t *testing.T) {
	ws := newWebmcpServer(t)
	ctx := webmcpBrowserCtx(t)

	if err := chromedp.Run(ctx, chromedp.Navigate(ws.srv.URL+"/")); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	var has bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`("modelContext" in document) || ("modelContext" in navigator)`, &has)); err != nil {
		t.Fatalf("feature probe: %v", err)
	}
	if !has {
		t.Skip("modelContext absent even with --enable-blink-features=WebMCP; browser predates WebMCP (needs Chromium 146+)")
	}
	waitForTools(t, ctx, "broken,echo_upper,get_app_instructions,grouped_probe,proto_probe,search")

	// Stash the token, then remove the meta tag entirely.
	var token string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const m = document.querySelector('meta[name="csrf-token"]');
		const v = m.content;
		m.remove();
		return v;
	})()`, &token)); err != nil {
		t.Fatalf("remove meta: %v", err)
	}
	if token == "" {
		t.Fatal("page rendered an empty CSRF token")
	}

	var res string
	if err := evalAwait(ctx, execToolExpr("echo_upper", `{msg:"blocked"}`), &res); err != nil {
		t.Fatalf("execute without meta: %v", err)
	}
	assertToolResult(t, res, true, "csrf")

	// Restore the meta tag; the very next dispatch picks it up.
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`(() => {
		const m = document.createElement('meta');
		m.name = 'csrf-token';
		m.content = %q;
		document.head.appendChild(m);
		return true;
	})()`, token), &has)); err != nil {
		t.Fatalf("restore meta: %v", err)
	}
	if err := evalAwait(ctx, execToolExpr("echo_upper", `{msg:"ok"}`), &res); err != nil {
		t.Fatalf("execute with restored meta: %v", err)
	}
	assertToolResult(t, res, false, `{"msg":"OK"}`)
}

// execToolExpr looks the named tool handle up via getTools and executes
// it with the given input object literal. executeTool takes the
// RegisteredTool handle plus the input as a JSON string, and resolves
// with the execute() return value JSON-stringified (Chromium 151
// behavior, established empirically).
func execToolExpr(name, inputLiteral string) string {
	return fmt.Sprintf(`(async () => {
		const mc = document.modelContext || navigator.modelContext;
		const tools = await mc.getTools();
		const tool = tools.find(t => t.name === %q);
		if (!tool) return JSON.stringify({missing: true});
		return mc.executeTool(tool, JSON.stringify(%s));
	})()`, name, inputLiteral)
}

func assertToolResult(t *testing.T, raw string, wantErr bool, wantText string) {
	t.Helper()
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("tool result not JSON: %v\nraw: %s", err, raw)
	}
	if res.IsError != wantErr {
		t.Fatalf("isError = %v, want %v; raw: %s", res.IsError, wantErr, raw)
	}
	if len(res.Content) != 1 || res.Content[0].Type != "text" ||
		!strings.Contains(res.Content[0].Text, wantText) {
		t.Fatalf("content = %+v, want text containing %q", res.Content, wantText)
	}
}
