// Package webmcp exposes an app's server-declared tools to in-browser AI
// agents through the experimental WebMCP proposal (document.modelContext).
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
	corestatic "github.com/DonaldMurillo/gofastr/core/static"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

const (
	// ScriptRoute serves the bridge script with the tool manifest baked in.
	ScriptRoute = "/__gofastr/webmcp.js"

	// ManifestRoute serves the frozen tool manifest as JSON.
	ManifestRoute = "/__gofastr/webmcp/tools.json"
)

const defaultInputSchema = `{"type":"object","properties":{}}`

//go:embed bridge.js
var bridgeJS string

//go:embed third_party/webmcp-polyfill/polyfill.js
var polyfillJS string

var toolNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

const toolsPlaceholder = "__GOFASTR_WEBMCP_TOOLS__"

const debugPlaceholder = "__GOFASTR_WEBMCP_DEBUG__"

// polyfillPlaceholder is replaced with the optional browser bundle.
const polyfillPlaceholder = "__GOFASTR_WEBMCP_POLYFILL__"

// Tool declares one WebMCP tool: what the agent sees and the same-origin
// endpoint its execute() calls with the visitor's credentials.
type Tool struct {
	// Name is unique on the page and matches [A-Za-z0-9_.-]{1,128}.
	Name string `json:"name"`

	// Group names the Host.Group the tool belongs to; it is metadata only.
	Group string `json:"group,omitempty"`

	// Title is an optional human-readable display name.
	Title string `json:"title,omitempty"`

	// Description tells the agent what the tool does; it is required.
	Description string `json:"description"`

	// InputSchema is a JSON Schema object; it defaults to an empty object schema.
	InputSchema json.RawMessage `json:"inputSchema"`

	// Examples are usage examples checked against InputSchema at registration.
	Examples []Example `json:"examples,omitempty"`

	// OutputSchema documents the HTTP response body in the manifest.
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`

	// Method is GET, POST, PUT, PATCH or DELETE; it defaults to POST.
	Method string `json:"method"`

	// Path is the endpoint's same-origin absolute path; a query is allowed.
	Path string `json:"path"`

	// ReadOnlyHint is forwarded as annotations.readOnlyHint and is advisory only.
	ReadOnlyHint bool `json:"readOnlyHint,omitempty"`

	// UntrustedContentHint is forwarded as annotations.untrustedContentHint and is advisory only.
	UntrustedContentHint bool `json:"untrustedContentHint,omitempty"`

	// ConsequentialHint flags a tool whose execution can have significant
	// consequences. It is advisory to the agent and browser.
	ConsequentialHint bool `json:"consequentialHint,omitempty"`

	// Debugging marks a tool intended for developers or test tooling.
	Debugging bool `json:"debugging,omitempty"`
}

// ScriptRegistrar puts the bridge script on every full-page render; *uihost.UIHost satisfies it.
type ScriptRegistrar interface {
	RegisterExternalScript(src string) error
}

type documentScriptRegistrar interface {
	RegisterDocumentScript(src string, scope func(path string) bool) error
}

// Host collects tool declarations and, once mounted, serves the bridge
// script and manifest. Create one with New.
type Host struct {
	mu           sync.Mutex
	tools        []Tool
	names        map[string]bool
	mounted      bool
	observer     func(ToolEvent)
	instructions string
	groups       []*groupMeta
}

// New returns an empty Host configured by opts.
func New(opts ...HostOption) *Host {
	h := &Host{names: make(map[string]bool)}
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	return h
}

// Register adds a tool declaration or returns an error naming the field it
// refuses; after Mount it always refuses.
func (h *Host) Register(t Tool) error {
	declared := t
	t, class, err := validateTool(t)
	if err == nil {
		h.mu.Lock()
		class, err = h.admitLocked("Register", t)
		if err == nil {
			h.names[t.Name] = true
			h.tools = append(h.tools, t)
		}
		h.mu.Unlock()
	}
	if err != nil {
		h.emitRegister(declared, class)
	}
	return err
}

func (h *Host) admitLocked(verb string, t Tool) (string, error) {
	switch {
	case h.mounted:
		return "after_mount", fmt.Errorf("webmcp: %s(%q) refused: Mount already froze the tool set; register every tool before Mount", verb, t.Name)
	case h.names[t.Name]:
		return "duplicate_name", fmt.Errorf("webmcp: %s: duplicate tool name %q (the browser refuses duplicate registrations)", verb, t.Name)
	case h.instructions != "" && t.Name == InstructionsToolName:
		return "reserved_name", fmt.Errorf("webmcp: %s(%q) refused: WithInstructions reserves that name for the generated orientation tool; use a different name or drop WithInstructions", verb, t.Name)
	case t.Group != "" && !h.groupDeclaredLocked(t.Group):
		return "undeclared_group", fmt.Errorf("webmcp: %s(%q): references group %q, which was never declared with Host.Group; declare the group first", verb, t.Name, t.Group)
	}
	return "", nil
}

func validateTool(t Tool) (Tool, string, error) {
	if !toolNameRe.MatchString(t.Name) {
		return t, "tool_name", fmt.Errorf("webmcp: Register: tool name %q must match the WebMCP grammar [A-Za-z0-9_.-]{1,128}; the browser rejects anything else and the tool would silently never appear in getTools()", t.Name)
	}
	if strings.TrimSpace(t.Description) == "" {
		return t, "description", fmt.Errorf("webmcp: Register(%q): Description is required; an agent cannot pick an undescribed tool", t.Name)
	}
	switch t.Method {
	case "":
		t.Method = http.MethodPost
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return t, "method", fmt.Errorf("webmcp: Register(%q): unsupported method %q (GET, POST, PUT, PATCH, DELETE)", t.Name, t.Method)
	}
	if !validToolPath(t.Path) {
		return t, "path", fmt.Errorf("webmcp: Register(%q): Path %q must be a same-origin absolute path (\"/api/x\", query allowed; no scheme, host, fragment, backslash, control chars, or \".\"/\"..\" segments)", t.Name, t.Path)
	}
	if len(t.InputSchema) == 0 {
		t.InputSchema = json.RawMessage(defaultInputSchema)
	} else {
		t.InputSchema = append(json.RawMessage(nil), t.InputSchema...)
	}
	if !json.Valid(t.InputSchema) || !strings.HasPrefix(strings.TrimSpace(string(t.InputSchema)), "{") {
		return t, "input_schema", fmt.Errorf("webmcp: Register(%q): InputSchema must be a JSON object", t.Name)
	}
	if class, err := validateMetadata(&t); err != nil {
		return t, class, err
	}
	return t, "", nil
}

// Mount freezes the tool set, serves the bridge script, manifest and (with
// WithInstructions) instructions on rt, registers the script on scripts
// when it is non-nil, and returns the hash-versioned script URL.
func (h *Host) Mount(rt *router.Router, scripts ScriptRegistrar, opts ...MountOption) (string, error) {
	var cfg mountConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	h.mu.Lock()
	if h.mounted {
		h.mu.Unlock()
		return "", fmt.Errorf("webmcp: Mount called twice")
	}
	if len(h.tools) == 0 {
		h.mu.Unlock()
		return "", fmt.Errorf("webmcp: Mount refused with zero registered tools: the bridge script would do nothing; call Register first")
	}
	h.mounted = true
	if err := h.validateGroupsLocked(h.tools); err != nil {
		h.mounted = false
		h.mu.Unlock()
		return "", err
	}
	tools := h.tools
	instructions := h.instructions
	groups := h.groupInfos()
	if instructions != "" {
		tools = append(tools[:len(tools):len(tools)], orientationTool(rt.Prefix()))
	}
	h.mu.Unlock()
	fail := func(err error) (string, error) {
		h.mu.Lock()
		h.mounted = false
		h.mu.Unlock()
		return "", err
	}

	manifest, err := json.Marshal(struct {
		Instructions string      `json:"instructions,omitempty"`
		Groups       []groupInfo `json:"groups,omitempty"`
		Tools        []Tool      `json:"tools"`
	}{Instructions: instructions, Groups: groups, Tools: tools})
	if err != nil {
		return fail(fmt.Errorf("webmcp: Mount: marshal manifest: %w", err))
	}
	toolsJSON, err := json.Marshal(tools)
	if err != nil {
		return fail(fmt.Errorf("webmcp: Mount: marshal tools: %w", err))
	}
	quotedTools, err := json.Marshal(string(toolsJSON))
	if err != nil {
		return fail(fmt.Errorf("webmcp: Mount: quote tools manifest: %w", err))
	}
	for _, ph := range []string{toolsPlaceholder, debugPlaceholder, polyfillPlaceholder} {
		if !strings.Contains(bridgeJS, ph) {
			return fail(fmt.Errorf("webmcp: Mount: bridge.js is missing the %s placeholder; the embedded asset is corrupt", ph))
		}
	}
	debugLit := "false"
	if cfg.bridgeDebug {
		debugLit = "true"
	}
	var polyfill string
	if cfg.polyfill {
		polyfill = polyfillJS
	}
	script := []byte(strings.NewReplacer(
		toolsPlaceholder, string(quotedTools),
		debugPlaceholder, debugLit,
		polyfillPlaceholder, polyfill,
	).Replace(bridgeJS))

	scriptPath := rt.Prefix() + ScriptRoute
	manifestPath := rt.Prefix() + ManifestRoute
	instructionsPath := rt.Prefix() + InstructionsRoute

	for _, route := range rt.Routes() {
		if route.Method != http.MethodGet {
			continue
		}
		conflict := route.Pattern == scriptPath || route.Pattern == manifestPath ||
			(instructions != "" && route.Pattern == instructionsPath)
		if conflict {
			return fail(fmt.Errorf("webmcp: Mount: %s is already registered on this router (a second webmcp Host on the same router? mount one Host per router)", route.Pattern))
		}
	}

	scriptURL := uihost.ScriptURL(scriptPath, script)
	if cfg.docScope != nil {
		if scripts == nil {
			return fail(fmt.Errorf("webmcp: Mount: WithDocumentScope needs the host registrar (pass the *uihost.UIHost, not nil); a self-rendered tag cannot declare the boundary in the route manifest"))
		}
		dsr, ok := scripts.(documentScriptRegistrar)
		if !ok {
			return fail(fmt.Errorf("webmcp: Mount: WithDocumentScope needs a registrar with RegisterDocumentScript (a *uihost.UIHost); got %T", scripts))
		}
		if err := dsr.RegisterDocumentScript(scriptURL, cfg.docScope); err != nil {
			return fail(fmt.Errorf("webmcp: Mount: register document-scoped bridge script: %w", err))
		}
	} else if scripts != nil {
		if err := scripts.RegisterExternalScript(scriptURL); err != nil {
			return fail(fmt.Errorf("webmcp: Mount: register bridge script: %w", err))
		}
	}

	private := cfg.requesterDependent()
	scriptH := http.Handler(uihost.ScriptHandler(script))
	manifestH := http.Handler(manifestHandler(manifest))
	if private {
		scriptH = privateTextHandler("application/javascript; charset=utf-8", string(script), privateETag(script))
		manifestH = privateTextHandler("application/json; charset=utf-8", string(manifest), privateETag(manifest))
	}
	rt.Get(ScriptRoute, cfg.wrap(scriptH, "application/javascript; charset=utf-8", ""))
	rt.Get(ManifestRoute, cfg.wrap(manifestH, "application/json; charset=utf-8", `{"tools":[]}`))
	if instructions != "" {
		body, err := json.Marshal(struct {
			Instructions string `json:"instructions"`
		}{Instructions: instructions})
		if err != nil {
			return fail(fmt.Errorf("webmcp: Mount: marshal instructions: %w", err))
		}
		instrH := http.Handler(manifestHandler(body))
		if private {
			instrH = privateTextHandler("application/json; charset=utf-8", string(body), privateETag(body))
		}
		rt.Get(InstructionsRoute, cfg.wrap(instrH, "application/json; charset=utf-8", `{"instructions":""}`))
	}
	return scriptURL, nil
}

func manifestHandler(body []byte) http.Handler {
	etag := fmt.Sprintf("%q", fmt.Sprintf("%x", len(body))+"-"+hashHex(body))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Type", "application/json; charset=utf-8")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-cache")
		h.Set("ETag", etag)
		if corestatic.IfNoneMatch(r, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(body)
	})
}

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
	pathOnly := pathPortion(p)
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
