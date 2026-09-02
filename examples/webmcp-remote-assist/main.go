// Package main is the reference example for authenticated WebMCP plus
// WebRTC remote support: one Go binary, one origin, two roles.
//
// The boundaries it exists to teach:
//
//   - Support-only WebMCP discovery. The bridge rides the document
//     script rail scoped to /support (webmcp.WithDocumentScope), the
//     assets are authorization-wrapped (WithAssetAuthorization), and
//     leaving the console is a real navigation, so a document never
//     carries tools it should not.
//   - Role cookies authorize. Every endpoint re-checks the support or
//     operator cookie; the WebMCP marker header only attributes.
//   - One typed command. The console's manual button and the
//     send_instruction / clear_instruction tools decode into one
//     assistCommand and share applyCommand.
//   - Peer-to-peer media. getUserMedia (video, never audio) feeds an
//     RTCPeerConnection whose SDP and ICE cross the server only as
//     signaling frames on the session's WebSocket.
//   - Sequenced realtime state. core/stream.StateChannel server-side,
//     __gofastr.loadModule('ws') + createSequencedReducer in the
//     browser, so a reconnect cannot resurrect stale instruction state.
//
// Run it:
//
//	go run ./examples/webmcp-remote-assist
//
// then open http://localhost:8090. README.md walks the whole flow.
package main

import (
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/experimental/webmcp"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

//go:embed static/app.js
var appJS []byte

// assist is the example's store. One global, set in main; tests build
// their own via buildApp and reset it per server.
var assist *assistApp

const listenAddr = ":8090"

func main() {
	fwApp := buildApp()
	if os.Getenv("ASSIST_SUPPORT_KEY") == "" {
		log.Printf("support sign-in key (set ASSIST_SUPPORT_KEY to choose one): %s", assist.supportKey)
	}
	log.Printf("webmcp-remote-assist listening on http://localhost%s", listenAddr)
	if err := fwApp.Start(listenAddr); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// buildApp wires the site, the host, the routes, and the WebMCP mount,
// without binding a port, so tests drive fwApp.Router() directly.
func buildApp() *framework.App {
	assist = newAssistApp()
	site := uiapp.NewApp("remote-assist")
	// The canonical theme: light and dark palettes from one token set,
	// so the pages follow the operator's OS preference with no CSS here.
	site.WithTheme(theme.Default())
	layout := uiapp.NewLayout("main").WithHeader(&siteHeaderComponent{})
	site.SetDefaultLayout(layout)

	site.RegisterScreen(uiapp.NewScreen("/", &LandingScreen{}).WithTitle("Remote assist"), nil)
	site.RegisterScreen(uiapp.NewScreen("/support/login", &SupportLoginScreen{}).WithTitle("Support sign in"), nil)
	site.RegisterScreen(uiapp.NewScreen("/support", &SupportHomeScreen{}).WithTitle("Assist sessions"), nil)
	site.RegisterScreen(uiapp.NewScreen("/support/session/{id}", &SupportConsoleScreen{}).WithTitle("Support console"), nil)
	site.RegisterScreen(uiapp.NewScreen("/session/{id}", &OperatorScreen{}).WithTitle("Assist session"), nil)

	host := uihost.New(site,
		// Every dead end is the same branded page: unknown session,
		// expired, spent join link. Callers never learn which.
		uihost.WithNotFoundScreen(&SessionGoneScreen{}),
	)

	fwApp := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "webmcp-remote-assist"}),
		// The default Permissions-Policy denies the camera outright;
		// the operator page needs it, so the policy opens camera to
		// this origin only. The microphone stays denied at the header
		// level: "no audio" is enforced by the browser, not just by
		// the getUserMedia constraints in app.js.
		framework.WithSecurityHeaders(middleware.SecurityHeadersConfig{
			PermissionsPolicy: "geolocation=(), microphone=(), camera=(self)",
		}),
	)
	// Route parameters before the guards that read them (ui-wiring:
	// "Guards on dynamic screens"). Router.Use runs first-registered
	// outermost; fwApp.Use appends.
	fwApp.Use(host.RouteMatchMiddleware())
	fwApp.Use(assist.guard(host))
	fwApp.Mount(host)

	rt := fwApp.Router()

	// ── Auth and session pages ────────────────────────────────────
	// Every write route lives in a group that carries its guard, so a
	// route added later inherits it (the shape the contracts pipeline
	// checks). The sign-in is the credential exchange itself: it mints
	// nothing without the key and is same-origin checked.
	rt.Post("/support/login", sameOrigin(assist.handleLogin())) //gofastr:allow(GOFASTR1902) the sign-in exchanges the key for the role cookie; it is the guard the other routes rely on
	support := rt.Group("/support", sameOrigin, assist.requireSupport())
	support.Post("/sessions", assist.handleCreateSession())
	support.Post("/session/{id}/instruction", assist.handleManualForm(cmdInstruction))
	support.Post("/session/{id}/clear", assist.handleManualForm(cmdClear))
	operator := rt.Group("/session", sameOrigin, assist.requireOperator())
	operator.Post("/{id}/ack", assist.handleAck())
	rt.Get("/join/{token}", assist.handleJoin(host))

	// ── Realtime: one channel per session, two role-scoped sockets ─
	rt.Get("/support/session/{id}/ws", assist.handleWS(roleSupport))
	rt.Get("/session/{id}/ws", assist.handleWS(roleOperator))

	// ── The one browser script, served CSP-safe and hash-versioned,
	// on the document rail for the session pages only ────────────────
	rt.Get("/__assist/app.js", uihost.ScriptHandler(appJS))
	if err := host.RegisterDocumentScript(uihost.ScriptURL("/__assist/app.js", appJS), assistDocScope); err != nil {
		log.Fatalf("assist: register document script: %v", err)
	}

	// ── WebMCP: browser tools for the support role ────────────────
	tools := webmcp.New(
		webmcp.WithInstructions("Inspect before mutating. Send one instruction at a time; "+
			"send_instruction replaces the previous one. An HTTP 200 means the command was "+
			"accepted, not that the operator saw it: verify delivery from inspect_session's "+
			"acked field, which the operator sets by acknowledging the rendered instruction."),
		webmcp.WithObserver(assist.observeToolEvent),
	)
	group := tools.Group("assist",
		webmcp.WithDescription("Remote assist controls for the signed-in support operator."),
		webmcp.WithPreferredFirst("inspect_session"),
	)
	toolAuth := []router.Middleware{assist.requireSupport(), sameOrigin}
	for _, decl := range []struct {
		tool    webmcp.Tool
		handler http.Handler
	}{
		{assist.inspectTool(), assist.handleInspect()},
		{assist.sendTool(), assist.handleSend()},
		{assist.clearTool(), assist.handleClearTool()},
	} {
		if err := group.Handle(rt, decl.tool, decl.handler, webmcp.WithHTTPMiddleware(toolAuth...)); err != nil {
			log.Fatalf("assist: webmcp handle %s: %v", decl.tool.Name, err)
		}
	}
	if _, err := tools.Mount(rt, host,
		// The bridge exists only on support documents; entering or
		// leaving /support is a real navigation (the host's SPA
		// runtime turns the scope edge into a document boundary), so
		// an operator page can never inherit live tools.
		webmcp.WithDocumentScope(supportScope),
		// Discovery is an authority surface: the manifest names every
		// tool and endpoint, so the assets need the same role check
		// the endpoints have. WithPageScope is defense in depth for a
		// stray script tag; the cookie check is the decision.
		webmcp.WithAssetAuthorization(assist.requireSupport()),
		webmcp.WithPageScope(func(r *http.Request) bool { return assist.authorizeSupport(r) }),
		webmcp.WithPrivateAssets(),
		// Bounded bridge state (support, registered names, last
		// status; never inputs or URLs) so the console can show
		// whether the browser registered the tools.
		webmcp.WithBridgeDebug(),
	); err != nil {
		log.Fatalf("assist: webmcp mount: %v", err)
	}

	return fwApp
}

// supportScope is the WebMCP document scope: the signed-in support
// page tree. Written prefix-style so the concrete path
// (/support/session/ab12) and the route pattern (/support/session/:id)
// agree. The sign-in page sits under /support but is anonymous, so it
// is outside the scope: a document with no role carries no bridge.
func supportScope(path string) bool {
	if path == "/support/login" {
		return false
	}
	return path == "/support" || strings.HasPrefix(path, "/support/")
}

// assistDocScope is the app.js document scope: both session pages.
// The operator page and the support console each need the WebSocket
// client; the landing page, the sign-in page, and the join exchange
// carry nothing.
func assistDocScope(path string) bool {
	return supportScope(path) || path == "/session" || strings.HasPrefix(path, "/session/")
}

// siteHeaderComponent is the shared chrome. Its nav link to "/" is the
// visible way out of the console: crossing the document scope is a
// full navigation, which is what retires the tools.
type siteHeaderComponent struct{}

func (h *siteHeaderComponent) Render() render.HTML {
	return ui.SiteHeader(ui.SiteHeaderConfig{
		Brand: ui.Link(ui.LinkConfig{Href: "/", Text: "Remote assist"}),
		NavItems: []ui.SiteHeaderLink{
			{Label: "Overview", Href: "/"},
			{Label: "Support", Href: "/support"},
		},
	})
}

// guard answers role failures with real pages (ui-wiring's recovery
// screen pattern): support pages redirect to the demo sign-in, the
// operator page and every dead session render the one SessionGone
// screen with 410. An unauthorized caller never learns whether a
// session exists.
func (a *assistApp) guard(host *uihost.UIHost) router.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			m, ok := uiapp.MatchFromContext(r.Context())
			if ok {
				switch m.ScreenID() {
				case "/support", "/support/session/:id":
					if !a.authorizeSupport(r) {
						http.Redirect(w, r, "/support/login", http.StatusSeeOther)
						return
					}
					if m.ScreenID() == "/support/session/:id" && a.lookup(m.Param("id")) == nil {
						host.RenderScreen(w, r, &SessionGoneScreen{}, uihost.ScreenResponse{Status: http.StatusGone})
						return
					}
				case "/session/:id":
					if !a.authorizeOperator(operatorCookie(r), m.Param("id")) || a.lookup(m.Param("id")) == nil {
						host.RenderScreen(w, r, &SessionGoneScreen{}, uihost.ScreenResponse{Status: http.StatusGone})
						return
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// handleLogin mints the support role cookie when the form carries the
// sign-in key. Demo-only: a real app puts battery/auth here. The cookie
// is HttpOnly and Path=/ — the WebMCP assets live under /__gofastr/,
// outside the /support tree. A wrong key re-renders the sign-in page
// rather than answering with a distinguishable error.
func (a *assistApp) handleLogin() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || !a.checkSupportKey(r.PostFormValue("key")) {
			http.Redirect(w, r, "/support/login", http.StatusSeeOther)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     supportCookieName,
			Value:    a.mintSupportCookie(),
			Path:     "/",
			HttpOnly: true,
			Secure:   secureRequest(r),
			SameSite: http.SameSiteLaxMode,
			MaxAge:   int(cookieTTL.Seconds()),
		})
		http.Redirect(w, r, "/support", http.StatusSeeOther)
	})
}

// handleCreateSession starts a session and opens its console.
func (a *assistApp) handleCreateSession() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := a.createSession()
		http.Redirect(w, r, "/support/session/"+s.id, http.StatusSeeOther)
	})
}

// handleManualForm is the console's visible button: form-encoded POST
// decoded into the same assistCommand the AI tools use. It answers
// 303 back to the console, which is correct for a plain form post and
// harmless for app.js's fetch enhancement.
func (a *assistApp) handleManualForm(kind string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sid := router.Param(r, "id")
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		cmd := assistCommand{Session: sid, Kind: kind, Instruction: r.PostFormValue("instruction")}
		if _, status, err := a.applyCommand(cmd); err != nil {
			http.Error(w, err.Error(), status)
			return
		}
		http.Redirect(w, r, "/support/session/"+sid, http.StatusSeeOther)
	})
}

// handleAck records the operator's acknowledgement of the rendered
// instruction and returns to the page. The operator group's
// requireOperator middleware has already matched the cookie to {id}.
func (a *assistApp) handleAck() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sid := router.Param(r, "id")
		if _, status, err := a.applyCommand(assistCommand{Session: sid, Kind: cmdAck}); err != nil {
			http.Error(w, err.Error(), status)
			return
		}
		http.Redirect(w, r, "/session/"+sid, http.StatusSeeOther)
	})
}

// handleJoin exchanges the one-time link for the operator cookie and
// redirects to the operator page. A spent or unknown token renders the
// same 410 screen a dead session does.
func (a *assistApp) handleJoin(host *uihost.UIHost) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sid, cookieValue, ok := a.exchangeJoin(router.Param(r, "token"))
		if !ok {
			host.RenderScreen(w, r, &SessionGoneScreen{}, uihost.ScreenResponse{Status: http.StatusGone})
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     operatorCookieName,
			Value:    cookieValue,
			Path:     "/session", // never rides on a support URL
			HttpOnly: true,
			Secure:   secureRequest(r),
			SameSite: http.SameSiteLaxMode,
			MaxAge:   int(sessionTTL.Seconds()),
		})
		http.Redirect(w, r, "/session/"+sid, http.StatusSeeOther)
	})
}

// secureRequest reports whether the request arrived over TLS, directly
// or through a proxy that says so, which is the same signal the
// framework's CSRF middleware and battery/auth use to decide a cookie's
// Secure flag per request: on plain-HTTP localhost the cookie works,
// behind TLS it is never sent in clear.
func secureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// ── WebMCP tool declarations ─────────────────────────────────────

func (a *assistApp) inspectTool() webmcp.Tool {
	return webmcp.Tool{
		Name:        "inspect_session",
		Title:       "Inspect assist session",
		Description: "Read the session's current state: instruction text, whether the operator acknowledged it, who is connected, and whether the video path is up. Inspect before mutating.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"session":{"type":"string","description":"session id from the console URL"}},"required":["session"]}`),
		Method:      http.MethodGet,
		Path:        "/support/api/inspect",
		Examples: []webmcp.Example{{
			Summary: "Check delivery of the last instruction",
			Input:   json.RawMessage(`{"session":"ab12cd34"}`),
		}},
		ReadOnlyHint:         true,
		UntrustedContentHint: true, // instruction text is user content
	}
}

func (a *assistApp) sendTool() webmcp.Tool {
	return webmcp.Tool{
		Name:        "send_instruction",
		Title:       "Send instruction",
		Description: "Show one instruction on the operator's page. Replaces any previous instruction. Accepted (HTTP 200) is not delivered: the operator acknowledges the rendered text, visible via inspect_session.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"session":{"type":"string"},"instruction":{"type":"string","maxLength":500}},"required":["session","instruction"]}`),
		Method:      http.MethodPost,
		Path:        "/support/api/instruction",
		Examples: []webmcp.Example{{
			Summary: "Point at the restart control",
			Input:   json.RawMessage(`{"session":"ab12cd34","instruction":"Press and hold the green restart button for three seconds."}`),
		}},
	}
}

func (a *assistApp) clearTool() webmcp.Tool {
	return webmcp.Tool{
		Name:        "clear_instruction",
		Title:       "Clear instruction",
		Description: "Remove the current instruction from the operator's page.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"session":{"type":"string"}},"required":["session"]}`),
		Method:      http.MethodPost,
		Path:        "/support/api/clear",
	}
}

// ── WebMCP tool handlers ─────────────────────────────────────────
//
// Each decodes into assistCommand and calls applyCommand — the exact
// path the manual form takes. The invocation ref (the marked call's
// correlation id) rides along so the operator's acknowledgement can be
// correlated with the agent command that earned it.

func (a *assistApp) handleInspect() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := a.lookup(r.URL.Query().Get("session"))
		if s == nil {
			writeJSON(w, http.StatusGone, map[string]any{"error": "session has ended"})
			return
		}
		snap := a.snapshotFor(roleSupport, s)
		writeJSON(w, http.StatusOK, snap)
	})
}

func (a *assistApp) handleSend() http.Handler {
	return a.toolCommandHandler(func(in toolInput) assistCommand {
		return assistCommand{Session: in.Session, Kind: cmdInstruction, Instruction: in.Instruction}
	})
}

func (a *assistApp) handleClearTool() http.Handler {
	return a.toolCommandHandler(func(in toolInput) assistCommand {
		return assistCommand{Session: in.Session, Kind: cmdClear}
	})
}

type toolInput struct {
	Session     string `json:"session"`
	Instruction string `json:"instruction"`
}

func (a *assistApp) toolCommandHandler(build func(toolInput) assistCommand) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		var in toolInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
			return
		}
		cmd := build(in)
		cmd.Ref = invocationRef(r.Context())
		snap, status, err := a.applyCommand(cmd)
		if err != nil {
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		// Accepted, not delivered: the operator must still acknowledge.
		// inspect_session's acked field is the verification path.
		writeJSON(w, status, map[string]any{
			"accepted": true, "session": snap.Session,
			"verify": "call inspect_session and check acked",
		})
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
