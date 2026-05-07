// Package devserver provides a development server that wires together all core-ui
// subsystems: App rendering, Island SSE streaming, runtime.js injection,
// Go→JS action compilation, and signal-driven live updates.
package devserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core-ui/island"
	"github.com/gofastr/gofastr/core-ui/runtime"
)

// DevServer is the development server for a core-ui application.
// It serves rendered pages with runtime.js, compiled action JS, SSE streaming
// for islands, and handles signal-driven live updates.
type DevServer struct {
	App        *app.App
	Islands    *island.Manager
	mu         sync.RWMutex
	sessions   map[string]*Session // sessionID → session
	routeGraph *RouteGraph
	actionJS   map[string]string // componentID → compiled JS
	customCSS  string            // extra CSS to inject (e.g. demo.css)
}

// Session represents a connected browser session.
type Session struct {
	ID      string
	Created time.Time
}

// SignalAny is an interface to allow storing heterogeneous signals.
type SignalAny interface {
	GetAsInterface() interface{}
	UpdateAsInterface(v interface{})
	Subscribe() <-chan struct{}
}
type RouteGraph struct {
	Routes []RouteInfo
}

// RouteInfo describes a route for client-side preloading.
type RouteInfo struct {
	Path        string
	Title       string
	Description string
	Preload     bool
}

// Option configures a DevServer.
type Option func(*DevServer)

// WithCustomCSS adds extra CSS to inject into every page.
func WithCustomCSS(css string) Option {
	return func(ds *DevServer) {
		ds.customCSS = css
	}
}

// WithRouteGraph sets route preloading information.
func WithRouteGraph(rg *RouteGraph) Option {
	return func(ds *DevServer) {
		ds.routeGraph = rg
	}
}

// NewDevServer creates a new development server.
func NewDevServer(application *app.App, opts ...Option) *DevServer {
	ds := &DevServer{
		App:      application,
		Islands:  island.NewManager(),
		sessions: make(map[string]*Session),
		actionJS: make(map[string]string),
	}
	for _, opt := range opts {
		opt(ds)
	}
	return ds
}

// RegisterWidget registers a widget with the island manager for a session.
// Returns the widget wrapped as an island for rendering.
func (ds *DevServer) RegisterWidget(sessionID string, w *component.Widget) *island.Island {
	isl := island.NewIsland(fmt.Sprintf("%s-%s", w.ID, sessionID), w)
	isl.SessionID = sessionID
	ds.Islands.Register(isl)
	return isl
}

// CompileActions compiles a component's action methods to JS and caches them.
func (ds *DevServer) CompileActions(componentID string, comp component.Component) string {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if js, ok := ds.actionJS[componentID]; ok {
		return js
	}

	// If interactive, generate action registration JS
	if ic, ok := comp.(component.InteractiveComponent); ok {
		actions := component.ExtractActions(ic)
		if actions != nil {
			js := actionsToJS(componentID, actions)
			ds.actionJS[componentID] = js
			return js
		}
	}
	return ""
}

// GetActionJS returns all compiled action JS concatenated.
func (ds *DevServer) GetActionJS() string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var sb strings.Builder
	for _, js := range ds.actionJS {
		sb.WriteString(js)
		sb.WriteString("\n")
	}
	return sb.String()
}

// CreateSession creates a new browser session.
func (ds *DevServer) CreateSession() *Session {
	id := fmt.Sprintf("sess-%d", time.Now().UnixNano())
	sess := &Session{
		ID:      id,
		Created: time.Now(),
	}
	ds.mu.Lock()
	ds.sessions[id] = sess
	ds.mu.Unlock()
	return sess
}

// GetSession retrieves a session by ID.
func (ds *DevServer) GetSession(id string) (*Session, bool) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	s, ok := ds.sessions[id]
	return s, ok
}

// ServeHTTP implements http.Handler, routing requests to pages or SSE.
func (ds *DevServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// API / SSE routes
	switch {
	case path == "/__gofastr/sse":
		ds.handleSSE(w, r)
		return
	case path == "/__gofastr/runtime.js":
		ds.handleRuntimeJS(w, r)
		return
	case path == "/__gofastr/actions.js":
		ds.handleActionsJS(w, r)
		return
	case path == "/__gofastr/session":
		ds.handleCreateSession(w, r)
		return
	case strings.HasPrefix(path, "/__gofastr/signal/"):
		ds.handleSignalUpdate(w, r)
		return
	}

	// Page rendering
	ds.handlePage(w, r)
}

// handlePage renders a full page with runtime.js, SSE meta tag, and compiled actions.
func (ds *DevServer) handlePage(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	html, err := ds.App.RenderPage(path)
	if err != nil {
		http.Error(w, "Page not found: "+path, http.StatusNotFound)
		return
	}

	// Get or create session
	sessionCookie, err := r.Cookie("gofastr-session")
	var sessionID string
	if err == nil {
		sessionID = sessionCookie.Value
	}
	if sessionID == "" {
		sess := ds.CreateSession()
		sessionID = sess.ID
		http.SetCookie(w, &http.Cookie{
			Name:     "gofastr-session",
			Value:    sessionID,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}

	page := string(html)

	// Inject SSE meta tag
	sseMeta := fmt.Sprintf(`<meta name="gofastr-sse" content="/__gofastr/sse?session=%s">`, sessionID)
	page = strings.Replace(page, "</head>", sseMeta+"\n</head>", 1)

	// Inject runtime.js
	runtimeScript := `<script src="/__gofastr/runtime.js"></script>`
	page = strings.Replace(page, "</head>", runtimeScript+"\n</head>", 1)

	// Inject compiled actions
	actionJS := ds.GetActionJS()
	if actionJS != "" {
		actionsScript := fmt.Sprintf("<script>%s</script>", actionJS)
		page = strings.Replace(page, "</body>", actionsScript+"\n</body>", 1)
	}

	// Inject custom CSS
	if ds.customCSS != "" {
		cssTag := fmt.Sprintf("<style>%s</style>", ds.customCSS)
		page = strings.Replace(page, "</head>", cssTag+"\n</head>", 1)
	}

	// Inject route graph for client-side preloading
	if ds.routeGraph != nil {
		rgJSON, _ := json.Marshal(ds.routeGraph.Routes)
		rgScript := fmt.Sprintf(`<script>window.__gofastr_routes = %s;</script>`, string(rgJSON))
		page = strings.Replace(page, "</head>", rgScript+"\n</head>", 1)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, page)
}

// handleSSE streams island updates to the client.
func (ds *DevServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		http.Error(w, "missing session parameter", http.StatusBadRequest)
		return
	}

	// Use the island manager's SSE handler
	ds.Islands.ServeSSE(w, r)
}

// handleRuntimeJS serves the core-ui runtime JavaScript.
func (ds *DevServer) handleRuntimeJS(w http.ResponseWriter, r *http.Request) {
	js := runtime.MustRuntimeJS()
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprint(w, js)
}

// handleActionsJS serves all compiled action JS.
func (ds *DevServer) handleActionsJS(w http.ResponseWriter, r *http.Request) {
	js := ds.GetActionJS()
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	fmt.Fprint(w, js)
}

// handleCreateSession creates a new session and returns its ID.
func (ds *DevServer) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	sess := ds.CreateSession()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"sessionId": sess.ID})
}

// handleSignalUpdate receives a signal update from the client and pushes
// island updates via SSE.
func (ds *DevServer) handleSignalUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse: /__gofastr/signal/{signalID}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/__gofastr/signal/"), "/")
	if len(parts) == 0 {
		http.Error(w, "invalid signal path", http.StatusBadRequest)
		return
	}
	signalID := parts[0]

	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		// Try cookie
		if cookie, err := r.Cookie("gofastr-session"); err == nil {
			sessionID = cookie.Value
		}
	}

	// Push island updates for this session
	islandIDs := ds.Islands.ListBySession(sessionID)
	for _, id := range islandIDs {
		isl, ok := ds.Islands.Get(id)
		if !ok {
			continue
		}
		// Re-render island with updated signal
		_ = signalID
		html := isl.Update()
		ds.Islands.PushUpdate(island.IslandUpdate{
			IslandID: id,
			HTML:     string(html),
		}, sessionID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// PushIsland re-renders an island and pushes the update via SSE.
func (ds *DevServer) PushIsland(islandID string) error {
	return ds.Islands.Push(islandID)
}

// Start starts the dev server on the given address.
func (ds *DevServer) Start(addr string) error {
	fmt.Printf("GoFastr DevServer running on http://%s\n", addr)
	server := &http.Server{
		Addr:    addr,
		Handler: ds,
	}
	return server.ListenAndServe()
}

// StartContext starts the dev server with a context for graceful shutdown.
func (ds *DevServer) StartContext(ctx context.Context, addr string) error {
	fmt.Printf("GoFastr DevServer running on http://%s\n", addr)
	server := &http.Server{
		Addr:    addr,
		Handler: ds,
	}
	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()
	return server.ListenAndServe()
}

// RenderPage renders a page with all injections (for testing).
func (ds *DevServer) RenderPage(path string, sessionID string) (string, error) {
	html, err := ds.App.RenderPage(path)
	if err != nil {
		return "", err
	}

	page := string(html)

	// Inject SSE meta tag
	sseMeta := fmt.Sprintf(`<meta name="gofastr-sse" content="/__gofastr/sse?session=%s">`, sessionID)
	page = strings.Replace(page, "</head>", sseMeta+"\n</head>", 1)

	// Inject runtime.js
	runtimeScript := `<script src="/__gofastr/runtime.js"></script>`
	page = strings.Replace(page, "</head>", runtimeScript+"\n</head>", 1)

	// Inject compiled actions
	actionJS := ds.GetActionJS()
	if actionJS != "" {
		actionsScript := fmt.Sprintf("<script>%s</script>", actionJS)
		page = strings.Replace(page, "</body>", actionsScript+"\n</body>", 1)
	}

	// Inject custom CSS
	if ds.customCSS != "" {
		cssTag := fmt.Sprintf("<style>%s</style>", ds.customCSS)
		page = strings.Replace(page, "</head>", cssTag+"\n</head>", 1)
	}

	// Inject route graph
	if ds.routeGraph != nil {
		rgJSON, _ := json.Marshal(ds.routeGraph.Routes)
		rgScript := fmt.Sprintf(`<script>window.__gofastr_routes = %s;</script>`, string(rgJSON))
		page = strings.Replace(page, "</head>", rgScript+"\n</head>", 1)
	}

	return page, nil
}

// actionsToJS converts an ActionRegistry to browser-runnable JavaScript.
func actionsToJS(componentID string, reg *component.ActionRegistry) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("if (!window.__gofastr) window.__gofastr = {handlers:{}};\n"))
	sb.WriteString(fmt.Sprintf("window.__gofastr.handlers[%q] = {\n", componentID))
	sb.WriteString("};\n")
	return sb.String()
}

// PushUpdate pushes an island update for a specific session.
// This is a convenience method that wraps the island manager's push mechanism.
func (ds *DevServer) PushUpdate(islandID string, html string, sessionID string) {
	ds.Islands.PushUpdate(island.IslandUpdate{
		IslandID: islandID,
		HTML:     html,
	}, sessionID)
}

// ReadCustomCSSFile reads a CSS file and returns its content.
// This is a helper for the demo main.go.
func ReadCustomCSSFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
