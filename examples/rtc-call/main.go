// Package main is the dogfood example for battery/rtc: anonymous users,
// rooms by name, camera and microphone peer to peer, chat over a
// negotiated data channel. One binary, no accounts.
//
// The boundaries it exists to teach:
//
//   - Identity is a cookie. The lobby's form sets one HttpOnly cookie
//     (the display name); the room page and the signaler both read it
//     and nothing else. rtc.Join carries only what the server derived.
//   - The signaler is the battery. rtc.New + RegisterPlugin mounts the
//     WebSocket endpoint; the example writes no signaling code, unlike
//     webmcp-remote-assist, which hand-rolls its relay to teach the
//     underlying StateChannel shape.
//   - Every visible state is server-rendered. The room page ships the
//     local tile, one remote-tile template, a chat-line template, and
//     both pills of every status pair; static/app.js only flips
//     hidden, sets textContent and srcObject, and clones templates.
package main

import (
	_ "embed"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/rtc"
	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

//go:embed static/app.js
var appJS []byte

// callNameCookie carries the display name from the lobby to the room.
// HttpOnly: the page never reads it, the server does. SameSite=Lax so
// a cross-site top-level navigation still works, a cross-site
// WebSocket upgrade does not (browsers send no cookie on it).
const callNameCookie = "call_name"

// defaultAddr is the fallback when $PORT is unset; `gofastr dev` and
// PaaS runtimes inject PORT and isolation.ListenAddr honours it.
const defaultAddr = ":8091"

func main() {
	fwApp := buildApp()
	listenAddr, err := isolation.ListenAddr(".", defaultAddr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("rtc-call listening on %s", localURL(listenAddr))
	if err := fwApp.Start(listenAddr); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// buildApp wires the site, the host, the join route, and the rtc
// battery, without binding a port, so tests drive fwApp.Router()
// directly.
func buildApp() *framework.App {
	site := uiapp.NewApp("rtc-call")
	site.WithTheme(theme.Default())
	layout := uiapp.NewLayout("main").WithContainer().WithHeader(&siteHeaderComponent{})
	site.SetDefaultLayout(layout)

	site.RegisterScreen(uiapp.NewScreen("/", &LobbyScreen{}).WithTitle("Join a call"), nil)
	site.RegisterScreen(uiapp.NewScreen("/room/{room}", &RoomScreen{}).WithTitle("Call room"), nil)

	host := uihost.New(site)

	fwApp := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "rtc-call"}),
		// The framework's default Permissions-Policy denies camera and
		// microphone outright; a call needs both, opened to this origin
		// only, and the helper keeps the rest of the default intact.
		framework.WithSecurityHeaders(middleware.SecurityHeadersConfig{
			PermissionsPolicy: rtc.PermissionsPolicy(rtc.Camera, rtc.Microphone),
		}),
	)
	// Route parameters before the guards that read them (ui-wiring:
	// "Guards on dynamic screens"). Router.Use runs first-registered
	// outermost; fwApp.Use appends.
	fwApp.Use(host.RouteMatchMiddleware())
	fwApp.Use(roomGuard)
	fwApp.Mount(host)

	rt := fwApp.Router()
	// The lobby's credential exchange: an anonymous display name for a
	// cookie. Same-origin checked like every mutating route; invalid
	// input redirects back to the lobby and mints nothing.
	rt.Post("/join", sameOrigin(handleJoin())) //gofastr:allow(GOFASTR1902) the join exchange sets the anonymous display-name cookie; it is the gate the room page and the signaler's Authorize check

	// The signaling battery: rooms, addressed offer/answer/ICE relay,
	// ICE server config. Identity comes from authorize, never the wire.
	sig := rtc.New(rtc.Config{
		ICEServers: iceServersFromEnv(),
		TURN:       turnFromEnv(),
		Authorize:  authorize,
	})
	fwApp.RegisterPlugin(sig)

	// The one browser script, served CSP-safe and hash-versioned, on
	// the document rail for the room page only.
	rt.Get("/__call/app.js", uihost.ScriptHandler(appJS))
	if err := host.RegisterDocumentScript(uihost.ScriptURL("/__call/app.js", appJS), roomDocScope); err != nil {
		log.Fatalf("rtc-call: register document script: %v", err)
	}

	return fwApp
}

// siteHeaderComponent is the shared chrome.
type siteHeaderComponent struct{}

func (h *siteHeaderComponent) Render() render.HTML {
	return ui.SiteHeader(ui.SiteHeaderConfig{
		Brand: ui.Link(ui.LinkConfig{Href: "/", Text: "RTC call"}),
		NavItems: []ui.SiteHeaderLink{
			{Label: "Lobby", Href: "/"},
		},
	})
}

// roomDocScope is the app.js document scope: the room page only. The
// lobby carries no script; leaving a room (or entering one) is a real
// navigation, which is what tears the script and its sockets down.
func roomDocScope(path string) bool {
	return path == "/room" || strings.HasPrefix(path, "/room/")
}

// roomGuard refuses the room screen without the cookie or with a room
// name the join form would never have produced. Both failures answer
// the same redirect to the lobby, so a caller learns nothing about
// which rooms exist.
func roomGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m, ok := uiapp.MatchFromContext(r.Context()); ok && m.ScreenID() == "/room/:room" {
			c, err := r.Cookie(callNameCookie)
			if err != nil || c.Value == "" || !validName(c.Value) || !validRoom(m.Param("room")) {
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// handleJoin validates the lobby form, sets the display-name cookie,
// and redirects to the room.
func handleJoin() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Cap the form: two short fields, and an uncapped POST is a
		// memory lever on a public origin.
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		if err := r.ParseForm(); err != nil {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		name, room := r.PostFormValue("name"), r.PostFormValue("room")
		if !validName(name) || !validRoom(room) {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     callNameCookie,
			Value:    name,
			Path:     "/",
			HttpOnly: true,
			Secure:   secureRequest(r),
			SameSite: http.SameSiteLaxMode,
			MaxAge:   24 * 60 * 60, // one call session, not an account
		})
		http.Redirect(w, r, "/room/"+url.PathEscape(room), http.StatusSeeOther)
	})
}

// authorize is the signaler's gate: the display-name cookie decides
// WHO, the ?room= query decides WHERE. A client names nothing about
// itself on the wire that this function did not derive.
func authorize(r *http.Request) (rtc.Join, error) {
	c, err := r.Cookie(callNameCookie)
	if err != nil || c.Value == "" || !validName(c.Value) {
		return rtc.Join{}, &rtc.HTTPError{Status: http.StatusUnauthorized, Message: "join from the lobby first"}
	}
	room := r.URL.Query().Get("room")
	if !validRoom(room) {
		return rtc.Join{}, &rtc.HTTPError{Status: http.StatusBadRequest, Message: "bad room"}
	}
	return rtc.Join{Room: room, DisplayName: c.Value}, nil
}

// iceServersFromEnv reads CALL_STUN: unset means the default public
// STUN server, an explicitly empty value means none (both peers on one
// machine, host candidates only), anything else is the URL to use.
func iceServersFromEnv() []rtc.ICEServer {
	stun, ok := os.LookupEnv("CALL_STUN")
	if !ok {
		stun = "stun:stun.l.google.com:19302"
	}
	if stun == "" {
		return nil
	}
	return []rtc.ICEServer{{URLs: []string{stun}}}
}

// turnFromEnv reads CALL_TURN_URL and CALL_TURN_SECRET; both must be
// set or no TURN entry is configured. The secret stays in this
// process: the battery mints each peer its own short-lived credential.
func turnFromEnv() *rtc.TURN {
	u, secret := os.Getenv("CALL_TURN_URL"), os.Getenv("CALL_TURN_SECRET")
	if u == "" || secret == "" {
		return nil
	}
	return &rtc.TURN{URLs: []string{u}, Secret: secret}
}

// validRoom pins the room name the URL path carries: 1-40 bytes of
// lowercase letters, digits, and hyphens. The same rule at the join
// form, the room guard, and authorize means a room in a URL was always
// produced by the form.
func validRoom(room string) bool {
	if len(room) < 1 || len(room) > 40 {
		return false
	}
	for i := range len(room) {
		c := room[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// validName pins the display name: 1-40 bytes of printable ASCII
// (spaces allowed, control bytes refused), not only spaces. It lands
// in a cookie header value and in room rosters, so the bound is the
// defense, not a courtesy.
func validName(name string) bool {
	if len(name) < 1 || len(name) > 40 {
		return false
	}
	sawPrintable := false
	for i := range len(name) {
		c := name[i]
		if c < 0x20 || c > 0x7e {
			return false
		}
		if c != ' ' {
			sawPrintable = true
		}
	}
	return sawPrintable
}

// sameOrigin refuses cross-site mutating requests. It mirrors the
// convention battery/auth uses for its login forms (unexported there):
// Sec-Fetch-Site is the authoritative signal and is checked first;
// "cross-site" is refused outright, "same-origin" and "none" pass. The
// Origin host comparison is the fallback for clients without Fetch
// Metadata. A missing or "null" Origin passes: a legitimate top-level
// same-origin form navigation sends Origin: null (opaque origin), and
// curl never sends one. The cookie remains the authorization decision;
// this only answers WHERE FROM.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if crossSite(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func crossSite(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "cross-site":
		return true
	case "same-origin", "none":
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return true
	}
	return !strings.EqualFold(u.Host, r.Host)
}

// secureRequest reports whether the request arrived over TLS, directly
// or through a proxy that says so, which is the same signal the
// framework's CSRF middleware and battery/auth use to decide a cookie's
// Secure flag per request: on plain-HTTP localhost the cookie works,
// behind TLS it is never sent in clear.
func secureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// localURL renders a listen address as the URL to open: a bare ":8091"
// becomes http://localhost:8091, a host:port is used as is.
func localURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}
	return "http://" + addr
}
