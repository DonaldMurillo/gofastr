// Package devserver provides a development server that wires together all core-ui
// subsystems: App rendering, Island SSE streaming, runtime.js injection,
// Go→JS action compilation, and signal-driven live updates.
package devserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core-ui/island"
	"github.com/gofastr/gofastr/core-ui/runtime"
	"github.com/gofastr/gofastr/core-ui/style"
)

// DevServer is the development server for a core-ui application.
// It serves rendered pages with runtime.js, compiled action JS, SSE streaming
// for islands, and handles signal-driven live updates.
type DevServer struct {
	App        *app.App
	Islands    *island.Manager
	mu         sync.RWMutex
	sessions   map[string]*Session // sessionID → session
	actionJS   map[string]string   // componentID → compiled JS
	customCSS  string              // extra CSS to inject (e.g. demo.css)
	staticDir  string              // directory to serve static files from
	staticFS   fs.FS               // embedded filesystem for static files
	routeGraph *style.RouteGraph   // route graph for progressive CSS loading
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

// routeInfoJSON is the JSON shape sent to the browser as __gofastr_routes.
type routeInfoJSON struct {
	Path        string `json:"path"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Preload     bool   `json:"preload"`
	CSSChunk    string `json:"cssChunk,omitempty"`
}

// Option configures a DevServer.
type Option func(*DevServer)

// WithCustomCSS adds extra CSS to inject into every page.
func WithCustomCSS(css string) Option {
	return func(ds *DevServer) {
		ds.customCSS = css
	}
}

// WithRouteGraph sets the route graph for progressive CSS loading.
func WithRouteGraph(rg *style.RouteGraph) Option {
	return func(ds *DevServer) {
		ds.routeGraph = rg
	}
}

// WithStaticDir sets the directory to serve static files from.
func WithStaticDir(dir string) Option {
	return func(ds *DevServer) {
		ds.staticDir = dir
	}
}

// StaticDir returns the configured static directory path.
func (ds *DevServer) StaticDir() string {
	return ds.staticDir
}

// SetStaticFS sets an embedded filesystem for serving static files.
func (ds *DevServer) SetStaticFS(fsys fs.FS) {
	ds.staticFS = fsys
}

// HasStaticFS reports whether an embedded static FS is configured.
func (ds *DevServer) HasStaticFS() bool {
	return ds.staticFS != nil
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

// AutoCompileActions scans all registered screens and compiles actions for
// any that implement InteractiveComponent. The component ID is derived from
// ScreenComponentID.ComponentID() if implemented, otherwise from the route path.
func (ds *DevServer) AutoCompileActions() {
	for _, route := range ds.App.Routes() {
		screen, ok := ds.App.Router.Resolve(route.Path)
		if !ok {
			continue
		}
		if _, ok := screen.Component.(component.InteractiveComponent); ok {
			var id string
			if cid, ok := screen.Component.(app.ScreenComponentID); ok {
				id = cid.ComponentID()
			} else {
				id = pathToActionID(route.Path)
			}
			ds.CompileActions(id, screen.Component)
		}
	}
}

// pathToActionID derives a component action ID from a route path.
// "/" → "home", "/products" → "products", "/products/:slug" → "products-detail"
func pathToActionID(path string) string {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return "home"
	}
	// Replace / and : for valid JS identifiers
	name := strings.NewReplacer("/", "-", ":", "").Replace(path)
	return name
}

// buildRouteScript auto-builds the __gofastr_routes script from registered screens.
// CSS chunk names are auto-derived from the screen path unless overridden via RouteGraph.
func (ds *DevServer) buildRouteScript() string {
	routes := ds.App.Routes()
	if len(routes) == 0 {
		return ""
	}
	infos := make([]routeInfoJSON, len(routes))
	for i, r := range routes {
		info := routeInfoJSON{
			Path:        r.Path,
			Title:       r.Title,
			Description: r.Description,
			Preload:     i == 0, // preload first route
			CSSChunk:    pathToChunkName(r.Path),
		}
		// Allow RouteGraph to override chunk name
		if ds.routeGraph != nil {
			if ri, ok := ds.routeGraph.Routes[r.Path]; ok && ri.CSSChunk != "" {
				info.CSSChunk = ri.CSSChunk
			}
		}
		infos[i] = info
	}
	rgJSON, _ := json.Marshal(infos)
	return fmt.Sprintf(`<script>window.__gofastr_routes = %s;</script>`, string(rgJSON))
}

// pathToChunkName derives a CSS chunk filename from a route path.
// "/" → "home.css", "/about" → "about.css", "/products/:slug" → "products-slug.css"
func pathToChunkName(path string) string {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return "home.css"
	}
	// Replace / and : with - for valid filenames
	name := strings.NewReplacer("/", "-", ":", "").Replace(path)
	return name + ".css"
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
	case strings.HasPrefix(path, "/__gofastr/action"):
		ds.handleServerAction(w, r)
		return
	case strings.HasPrefix(path, "/__gofastr/widget/"):
		ds.handleWidgetJS(w, r)
		return
	case strings.HasPrefix(path, "/__gofastr/css/"):
		ds.handleCSSChunk(w, r)
		return
	}

	// Static file serving
	if path == "/favicon.ico" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if path != "/" {
		// Try filesystem directory first
		if ds.staticDir != "" {
			filePath := filepath.Join(ds.staticDir, path)
			if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
				http.ServeFile(w, r, filePath)
				return
			}
		}
		// Try embedded filesystem
		if ds.staticFS != nil {
			cleanPath := strings.TrimPrefix(path, "/")
			if cleanPath != "" {
				if f, err := ds.staticFS.Open(cleanPath); err == nil {
					f.Close()
					http.ServeFileFS(w, r, ds.staticFS, cleanPath)
					return
				}
			}
		}
	}

	// Page rendering
	ds.handlePage(w, r)
}

// handlePage renders a full page with runtime.js, SSE meta tag, and compiled actions.
func (ds *DevServer) handlePage(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Client-side navigation: return just the screen content (no layout)
	if r.Header.Get("X-Gofastr-Navigate") == "1" {
		ds.handlePartialPage(w, r, path)
		return
	}

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

	// Inject runtime.js before </body> so document.body is available
	runtimeScript := `<script src="/__gofastr/runtime.js"></script>`
	page = strings.Replace(page, "</body>", runtimeScript+"\n</body>", 1)

	// Inject compiled actions after runtime.js
	actionJS := ds.GetActionJS()
	if actionJS != "" {
		actionsScript := fmt.Sprintf("<script>%s</script>", actionJS)
		page = strings.Replace(page, "</body>", actionsScript+"\n</body>", 1)
	}

	// Inject custom CSS (theme CSS vars already in <head> from RenderPage)
	if ds.customCSS != "" {
		cssTag := fmt.Sprintf("<style>%s</style>", ds.customCSS)
		page = strings.Replace(page, "</head>", cssTag+"\n</head>", 1)
	}

	// Inject route graph (auto-built from registered screens)
	if rgScript := ds.buildRouteScript(); rgScript != "" {
		page = strings.Replace(page, "</head>", rgScript+"\n</head>", 1)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, page)
}

// handlePartialPage returns just the screen content for client-side navigation.
// The runtime.js router swaps the <main> content without a full page reload.
func (ds *DevServer) handlePartialPage(w http.ResponseWriter, r *http.Request, path string) {
	html, err := ds.App.RenderPartial(path)
	if err != nil {
		http.Error(w, "Page not found: "+path, http.StatusNotFound)
		return
	}

	// Look up screen title from route info
	if scr, ok := ds.App.Router.Resolve(path); ok && scr.Title != "" {
		title := scr.Title
		if title != "" {
			title = title + " — " + ds.App.Name
		}
		w.Header().Set("X-Gofastr-Title", title)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Gofastr-Partial", "true")
	fmt.Fprint(w, html)
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

// handleServerAction receives a server action invocation from the client.
// The client POSTs the action name and parameters; the server runs the
// handler in a goroutine and streams the result back via SSE.
func (ds *DevServer) handleServerAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Action  string            `json:"action"`
		Params  map[string]string `json:"params"`
		Session string            `json:"session"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	sessionID := body.Session
	if sessionID == "" {
		if cookie, err := r.Cookie("gofastr-session"); err == nil {
			sessionID = cookie.Value
		}
	}

	// Run the action handler and respond with a result
	// For now, push a toast notification via SSE
	if sessionID != "" {
		ds.Islands.PushUpdate(island.IslandUpdate{
			IslandID: "action-result",
			HTML:     fmt.Sprintf(`<div class="toast">Server processed: %s</div>`, body.Action),
		}, sessionID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"action":  body.Action,
		"message": "Server action processed",
	})
}

// handleWidgetJS serves compiled JavaScript for a specific widget.
// This enables lazy hydration: widgets load their behavior JS only on first interaction.
func (ds *DevServer) handleWidgetJS(w http.ResponseWriter, r *http.Request) {
	widgetID := strings.TrimPrefix(r.URL.Path, "/__gofastr/widget/")
	widgetID = strings.TrimSuffix(widgetID, ".js")

	ds.mu.RLock()
	js, ok := ds.actionJS[widgetID]
	ds.mu.RUnlock()

	if !ok {
		// Try to find by prefix match (e.g., "home-counter" matches "home-counter")
		ds.mu.RLock()
		for id, compiledJS := range ds.actionJS {
			if id == widgetID {
				js = compiledJS
				ok = true
				break
			}
		}
		ds.mu.RUnlock()
	}

	if !ok {
		http.Error(w, "widget not found: "+widgetID, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprint(w, js)
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

	// Inject runtime.js before </body> so document.body is available
	runtimeScript := `<script src="/__gofastr/runtime.js"></script>`
	page = strings.Replace(page, "</body>", runtimeScript+"\n</body>", 1)

	// Inject compiled actions after runtime.js
	actionJS := ds.GetActionJS()
	if actionJS != "" {
		actionsScript := fmt.Sprintf("<script>%s</script>", actionJS)
		page = strings.Replace(page, "</body>", actionsScript+"\n</body>", 1)
	}

	// Inject custom CSS (theme CSS vars already in <head> from App.RenderPage)
	if ds.customCSS != "" {
		cssTag := fmt.Sprintf("<style>%s</style>", ds.customCSS)
		page = strings.Replace(page, "</head>", cssTag+"\n</head>", 1)
	}

	// Inject route graph (auto-built from registered screens)
	if rgScript := ds.buildRouteScript(); rgScript != "" {
		page = strings.Replace(page, "</head>", rgScript+"\n</head>", 1)
	}

	return page, nil
}

// actionsToJS converts an ActionRegistry to browser-runnable JavaScript
// using the ClientJS field from each ActionDef. Each action's ClientJS
// is wrapped in a handler function and registered with the runtime.
func actionsToJS(componentID string, reg *component.ActionRegistry) string {
	if !reg.HasActions() {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("// Component: %s\n", componentID))
	sb.WriteString("(() => {\n")
	sb.WriteString(fmt.Sprintf("  const id = %q;\n", componentID))
	sb.WriteString("  const G = window.__gofastr;\n")
	sb.WriteString("  const handlers = {\n")

	first := true
	for _, action := range reg.All() {
		if !first {
			sb.WriteString(",\n")
		}
		first = false

		body := strings.TrimSpace(action.ClientJS)
		if body == "" {
			body = fmt.Sprintf("// no client handler for %q", action.Event)
		}

		sb.WriteString(fmt.Sprintf("    %q: (params) => {\n      %s\n    }", action.Event, body))
	}

	sb.WriteString("\n  };\n")
	sb.WriteString("  G.register(id, handlers);\n")
	sb.WriteString("})();\n")

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

// handleCSSChunk serves per-screen CSS chunks for progressive loading.
func (ds *DevServer) handleCSSChunk(w http.ResponseWriter, r *http.Request) {
	screenPath := strings.TrimPrefix(r.URL.Path, "/__gofastr/css")
	if screenPath == "" {
		screenPath = "/"
	}

	// In dev mode, serve the full custom CSS for any requested chunk.
	// In production, these would be pre-extracted per-screen CSS files.
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprint(w, ds.customCSS)
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
