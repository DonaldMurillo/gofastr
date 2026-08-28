// Package webmcp exposes an app's server-declared tools to in-browser AI
// agents through the WebMCP proposal (navigator.modelContext).
//
// EXPERIMENTAL. WebMCP itself is a Chrome origin-trial API (Chrome 146+
// behind chrome://flags/#enable-webmcp-testing, origin trial from Chrome
// 149; automation passes --enable-blink-features=WebMCP). The API surface
// of this package tracks that proposal and may change or be removed
// without a major version bump, like everything under
// framework/experimental.
//
// The server declares tools (name, description, JSON Schema, and the
// same-origin endpoint that implements them). Mount serves a small
// bridge script that registers each tool on navigator.modelContext; the
// tool's execute() proxies to the declared endpoint with the visitor's
// own session credentials. An in-browser agent therefore acts strictly
// as the signed-in user: every call re-enters the app through normal
// HTTP with the user's cookies, so auth, CSRF, owner scoping, and rate
// limits all apply unchanged. Only declare endpoints you would let the
// signed-in user call from their own devtools console, because that is
// exactly what a WebMCP tool call is.
//
// Usage:
//
//	h := webmcp.New()
//	err := h.Register(webmcp.Tool{
//	    Name:        "create_note",
//	    Description: "Create a note for the signed-in user.",
//	    InputSchema: json.RawMessage(`{"type":"object","properties":{"body":{"type":"string"}},"required":["body"]}`),
//	    Method:      "POST",
//	    Path:        "/api/notes",
//	})
//	// ...
//	scriptURL, err := h.Mount(app.Router(), uiHost)
//
// On a browser without WebMCP the bridge script is a no-op: it feature-
// detects navigator.modelContext and returns.
package webmcp

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

const (
	// ScriptRoute serves the bridge script with the tool manifest baked
	// in. The URL registered on the page carries a content-hash query so
	// it caches immutably and busts when the tool set changes.
	ScriptRoute = "/__gofastr/webmcp.js"

	// ManifestRoute serves the frozen tool manifest as JSON, for tests,
	// introspection, and server-side agents that want to know what the
	// page will register.
	ManifestRoute = "/__gofastr/webmcp/tools.json"
)

// defaultInputSchema is used when a Tool declares no InputSchema: a tool
// that takes an empty object.
const defaultInputSchema = `{"type":"object","properties":{}}`

//go:embed bridge.js
var bridgeJS string

// toolNameRe is the WebMCP spec's tool-name grammar (1–128 ASCII
// alphanumerics, "_", "-", "."). The browser enforces the same rule in
// registerTool and rejects violations asynchronously, which from the
// agent's viewpoint is a tool that silently never existed; failing at
// Register keeps the misdeclaration loud and server-side.
var toolNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

// toolsPlaceholder is the token in bridge.js the frozen manifest replaces.
const toolsPlaceholder = "__GOFASTR_WEBMCP_TOOLS__"

// Tool declares one WebMCP tool: what the agent sees (Name, Title,
// Description, InputSchema) and where execute() proxies to (Method,
// Path). The endpoint is called same-origin with the visitor's session
// credentials and a "X-Gofastr-WebMCP: 1" request header; its response
// body is returned to the agent verbatim as text content, with the MCP
// isError flag set on any non-2xx status.
type Tool struct {
	// Name uniquely identifies the tool on the page. Required, and
	// constrained to the WebMCP spec's tool-name grammar: 1–128 ASCII
	// alphanumerics, "_", "-", or "." (the browser rejects anything
	// else at registerTool time, silently from the agent's viewpoint,
	// so Register enforces it up front). Registration is per browsing
	// context, so the name must also be unique against any tools the
	// app registers through its own JS.
	Name string `json:"name"`

	// Title is an optional human-readable display name.
	Title string `json:"title,omitempty"`

	// Description tells the agent what the tool does and when to use
	// it. Required: an undescribed tool is dead weight the agent cannot
	// pick.
	Description string `json:"description"`

	// InputSchema is the JSON Schema for the tool's input, as a JSON
	// object. Defaults to {"type":"object","properties":{}}.
	InputSchema json.RawMessage `json:"inputSchema"`

	// Method is the HTTP method execute() uses: GET, POST, PUT, PATCH,
	// or DELETE. Defaults to POST. GET folds the input object into the
	// query string; every other method sends it as a JSON body.
	Method string `json:"method"`

	// Path is the same-origin absolute path of the implementing
	// endpoint ("/api/notes"). A query string is allowed; schemes,
	// hosts, fragments, backslashes, control characters, and "."/".."
	// segments are rejected.
	Path string `json:"path"`
}

// ScriptRegistrar is the seam through which Mount puts the bridge script
// on every full-page render. *uihost.UIHost satisfies it.
type ScriptRegistrar interface {
	RegisterExternalScript(src string) error
}

// Host collects tool declarations and, once mounted, serves the bridge
// script and manifest. Zero value is not usable; call New.
type Host struct {
	mu      sync.Mutex
	tools   []Tool
	names   map[string]bool
	mounted bool
}

// New returns an empty Host.
func New() *Host {
	return &Host{names: make(map[string]bool)}
}

// Register adds a tool declaration. It returns an error (naming the
// exact field) for a missing name or description, a duplicate name, an
// unsupported method, a path that is not a same-origin absolute path, or
// an InputSchema that is not a JSON object. Registration after Mount is
// refused: the script and manifest are frozen at Mount so every page
// render ships the same tool set.
func (h *Host) Register(t Tool) error {
	if !toolNameRe.MatchString(t.Name) {
		return fmt.Errorf("webmcp: Register: tool name %q must match the WebMCP grammar [A-Za-z0-9_.-]{1,128}; the browser rejects anything else and the tool would silently never appear in getTools()", t.Name)
	}
	if strings.TrimSpace(t.Description) == "" {
		return fmt.Errorf("webmcp: Register(%q): Description is required; an agent cannot pick an undescribed tool", t.Name)
	}
	switch t.Method {
	case "":
		t.Method = http.MethodPost
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return fmt.Errorf("webmcp: Register(%q): unsupported method %q (GET, POST, PUT, PATCH, DELETE)", t.Name, t.Method)
	}
	if !validToolPath(t.Path) {
		return fmt.Errorf("webmcp: Register(%q): Path %q must be a same-origin absolute path (\"/api/x\", query allowed; no scheme, host, fragment, backslash, control chars, or \".\"/\"..\" segments)", t.Name, t.Path)
	}
	if len(t.InputSchema) == 0 {
		t.InputSchema = json.RawMessage(defaultInputSchema)
	} else {
		// Clone: the caller owns the RawMessage's backing array, and a
		// later mutation of it would bypass the validation below and
		// corrupt the manifest frozen at Mount.
		t.InputSchema = append(json.RawMessage(nil), t.InputSchema...)
	}
	if !json.Valid(t.InputSchema) || !strings.HasPrefix(strings.TrimSpace(string(t.InputSchema)), "{") {
		return fmt.Errorf("webmcp: Register(%q): InputSchema must be a JSON object", t.Name)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.mounted {
		return fmt.Errorf("webmcp: Register(%q) refused: Mount already froze the tool set; register every tool before Mount", t.Name)
	}
	if h.names[t.Name] {
		return fmt.Errorf("webmcp: Register: duplicate tool name %q (the browser refuses duplicate registrations)", t.Name)
	}
	h.names[t.Name] = true
	h.tools = append(h.tools, t)
	return nil
}

// Mount freezes the tool set, registers ScriptRoute and ManifestRoute on
// rt, and (when scripts is non-nil) registers the hash-versioned script
// URL so every full-page render includes the bridge. It returns that
// script URL so hosts that render their own <script> tags can inject it
// themselves (pass scripts == nil in that case).
//
// Mounting with zero tools is an error: the wiring would silently do
// nothing. Mounting twice, or onto a router that already holds either
// route, is an error. A failed Mount (route conflict, bad embedded
// asset, script-rail refusal) registers nothing and leaves the Host
// re-mountable after the cause is fixed. On a grouped router the
// served routes and the returned script URL carry the group prefix.
func (h *Host) Mount(rt *router.Router, scripts ScriptRegistrar) (string, error) {
	h.mu.Lock()
	if h.mounted {
		h.mu.Unlock()
		return "", fmt.Errorf("webmcp: Mount called twice")
	}
	if len(h.tools) == 0 {
		h.mu.Unlock()
		return "", fmt.Errorf("webmcp: Mount refused with zero registered tools: the bridge script would do nothing; call Register first")
	}
	// Latch now so a concurrent Register can't grow the set mid-mount,
	// but un-latch on any failure below: everything fallible runs
	// before the first route registration, so a failed Mount leaves no
	// half-mounted state behind.
	h.mounted = true
	tools := h.tools
	h.mu.Unlock()
	fail := func(err error) (string, error) {
		h.mu.Lock()
		h.mounted = false
		h.mu.Unlock()
		return "", err
	}

	manifest, err := json.Marshal(struct {
		Tools []Tool `json:"tools"`
	}{Tools: tools})
	if err != nil {
		return fail(fmt.Errorf("webmcp: Mount: marshal manifest: %w", err))
	}
	toolsJSON, err := json.Marshal(tools)
	if err != nil {
		return fail(fmt.Errorf("webmcp: Mount: marshal tools: %w", err))
	}
	// The manifest is embedded as a QUOTED JSON string the bridge feeds
	// to JSON.parse, never as a bare JS object literal: in a literal,
	// "__proto__" is a prototype setter (an own key in the schema would
	// vanish) and duplicate "__proto__" keys — which json.Valid accepts
	// — are a SyntaxError that would kill the whole bridge. JSON.parse
	// has neither behavior.
	quotedTools, err := json.Marshal(string(toolsJSON))
	if err != nil {
		return fail(fmt.Errorf("webmcp: Mount: quote tools manifest: %w", err))
	}
	if !strings.Contains(bridgeJS, toolsPlaceholder) {
		return fail(fmt.Errorf("webmcp: Mount: bridge.js is missing the %s placeholder; the embedded asset is corrupt", toolsPlaceholder))
	}
	script := []byte(strings.Replace(bridgeJS, toolsPlaceholder, string(quotedTools), 1))

	// A grouped router prepends its prefix to every registration, so
	// the served paths (and therefore the rail URL and the conflict
	// pre-flight) must carry it too: without this, mounting on
	// Group("/v1") would serve the bridge at /v1/... while registering
	// the unprefixed URL on the script rail, a guaranteed 404.
	scriptPath := rt.Prefix() + ScriptRoute
	manifestPath := rt.Prefix() + ManifestRoute

	// Pre-flight the route patterns: the router panics on conflicts, so
	// a second Host on the same router (or an app squatting on the
	// routes) must surface as a clean error BEFORE the script rail is
	// touched, keeping "a failed Mount leaves nothing behind" true.
	// Routes() reports full (prefix-joined) patterns.
	for _, route := range rt.Routes() {
		if route.Method == http.MethodGet &&
			(route.Pattern == scriptPath || route.Pattern == manifestPath) {
			return fail(fmt.Errorf("webmcp: Mount: %s is already registered on this router (a second webmcp Host on the same router? mount one Host per router)", route.Pattern))
		}
	}

	scriptURL := uihost.ScriptURL(scriptPath, script)
	if scripts != nil {
		if err := scripts.RegisterExternalScript(scriptURL); err != nil {
			return fail(fmt.Errorf("webmcp: Mount: register bridge script: %w", err))
		}
	}

	rt.Get(ScriptRoute, uihost.ScriptHandler(script))
	rt.Get(ManifestRoute, manifestHandler(manifest))
	return scriptURL, nil
}

// manifestHandler serves the frozen manifest with a strong ETag.
func manifestHandler(body []byte) http.Handler {
	etag := fmt.Sprintf("%q", fmt.Sprintf("%x", len(body))+"-"+hashHex(body))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Type", "application/json; charset=utf-8")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-cache")
		h.Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(body)
	})
}

// hashHex is a small FNV-1a content hash for the manifest ETag. The
// script route's caching comes from uihost.ScriptHandler and is not
// affected by this.
func hashHex(b []byte) string {
	const (
		offset = 14695981039346656037
		prime  = 1099511628211
	)
	var h uint64 = offset
	for _, c := range b {
		h ^= uint64(c)
		h *= prime
	}
	return fmt.Sprintf("%016x", h)
}

// validToolPath mirrors the same-origin grammar uihost applies to
// external script srcs: single leading "/", no scheme or host, no
// backslash, no control bytes, no fragment, no "."/".." path segments,
// with the segment/backslash/control checks applied to BOTH the raw
// path and its percent-decoded form (browser URL normalization treats
// "%2e%2e" as "..", so a raw-only check would declare one path and
// fetch another); query strings allowed.
func validToolPath(p string) bool {
	if p == "" || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		return false
	}
	if strings.ContainsAny(p, "\\#") {
		return false
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	pathOnly := p
	if i := strings.IndexByte(p, '?'); i >= 0 {
		pathOnly = p[:i]
	}
	decoded, err := url.PathUnescape(pathOnly)
	if err != nil {
		return false
	}
	if strings.ContainsRune(decoded, '\\') {
		return false
	}
	for _, r := range decoded {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	for _, form := range []string{pathOnly, decoded} {
		for _, seg := range strings.Split(form, "/") {
			if seg == "." || seg == ".." {
				return false
			}
		}
	}
	return true
}
