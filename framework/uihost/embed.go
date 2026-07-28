package uihost

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// Embed route paths. The two API endpoints deliberately sit OUTSIDE the
// /__gofastr/embed/{surface} space: a surface named "exchange" would otherwise
// shadow the exchange endpoint, and "which of these two patterns wins" is not a
// question a security boundary should depend on.
const (
	embedLoaderPath   = fembed.LoaderPath
	embedRuntimePath  = fembed.RuntimePath
	embedExchangePath = fembed.ExchangePath
	embedRefreshPath  = fembed.RefreshPath
	embedShellPath    = fembed.SurfacePrefix + "{surface}"
	embedContentPath  = fembed.SurfacePrefix + "{surface}/content"
	embedGrantHeader  = fembed.GrantHeader
)

// maxEmbedThemeParam bounds the base64url theme parameter. It is checked before
// the value is used as a map key and again before it is decoded, because the
// route carrying it is unauthenticated and the value is attacker-chosen.
const maxEmbedThemeParam = 6 << 10

// embedThemeWait bounds how long a request waits for another request already
// resolving the same customer theme. Resolving one is a decode, an ApplyTokens
// and a stylesheet compose+hash — sub-millisecond in practice — so this is a
// ceiling for a pathological case, not a budget.
const embedThemeWait = 2 * time.Second

// WithEmbed serves the app's embeddable surfaces.
//
// The host is constructed with framework/embed.New; framework.App hands it
// HKDF-derived signing keys at mount time. Without keys the routes answer 503
// rather than minting anything, so a missing app secret is a loud failure
// instead of a silently unverifiable token.
func WithEmbed(h *fembed.Host) Option {
	return func(ds *UIHost) { ds.embedHost = h }
}

// EmbedHost returns the configured embed host, or nil.
func (ds *UIHost) EmbedHost() *fembed.Host { return ds.embedHost }

// embedThemeState tracks how many distinct customer themes a surface has
// registered, so a customer varying a token per page view cannot turn the
// variant registry into an unbounded map and the CSS cache into a miss
// generator. Past the cap the surface renders under the app theme.
type embedThemeState struct {
	mu sync.Mutex
	// resolved maps surface → the customer's raw theme parameter → the variant
	// key registered for it. It holds ONLY successful resolutions, and never
	// more than the surface's cap, so it is bounded by construction.
	//
	// Keying on the PARAMETER rather than on the resulting CSS hash is what
	// makes the cap enforceable: the cap has to be decided before anything is
	// registered, and the CSS hash is only known after a render. It also makes
	// the hot path free — a repeat request is one map read, no decode, no
	// ApplyTokens, no CSS render.
	resolved map[string]map[string]string
	// used stamps each resolved param with a monotonic counter so the cap can
	// evict the least recently used rather than refusing forever.
	used map[string]map[string]uint64
	tick uint64
	// pending holds one channel per in-flight reservation, closed by record or
	// release. A duplicate waits on it instead of resolving the same theme a
	// second time — the previous approach registered its own copy and released
	// it immediately, which dropped the variant outright when the duplicate got
	// there before the owner registered: refcount 1 → 0 → deleted, and the
	// duplicate returned a key nothing held.
	pending map[string]map[string]chan struct{}
}

// pendingLocked returns the in-flight channel for a reservation, creating it.
func (s *embedThemeState) pendingLocked(surface, param string) chan struct{} {
	if s.pending == nil {
		s.pending = map[string]map[string]chan struct{}{}
	}
	if s.pending[surface] == nil {
		s.pending[surface] = map[string]chan struct{}{}
	}
	ch, ok := s.pending[surface][param]
	if !ok {
		ch = make(chan struct{})
		s.pending[surface][param] = ch
	}
	return ch
}

// settleLocked closes and forgets a reservation's in-flight channel, releasing
// anyone waiting on it.
func (s *embedThemeState) settleLocked(surface, param string) {
	if byParam := s.pending[surface]; byParam != nil {
		if ch, ok := byParam[param]; ok {
			close(ch)
			delete(byParam, param)
		}
	}
}

// waitFor blocks until the in-flight reservation for param settles, or the
// timeout elapses. Returns the resolved key, if any.
func (s *embedThemeState) waitFor(surface, param string, timeout time.Duration) (string, bool) {
	s.mu.Lock()
	ch, inFlight := chanFor(s.pending[surface], param)
	s.mu.Unlock()
	if !inFlight {
		return s.lookup(surface, param)
	}
	select {
	case <-ch:
	case <-time.After(timeout):
	}
	return s.lookup(surface, param)
}

func chanFor(m map[string]chan struct{}, param string) (chan struct{}, bool) {
	if m == nil {
		return nil, false
	}
	ch, ok := m[param]
	return ch, ok
}

// lookup returns the variant key already RESOLVED for this surface + theme
// parameter.
//
// An in-flight reservation is stored as an empty string, and reporting that as
// a hit is what the duplicate handling in embedThemeKey exists to avoid: the
// caller would take the empty key and render under the app theme. The window is
// not small — the entry stays empty for the whole of resolveEmbedTheme, which
// decodes, validates, composes the stylesheet and hashes it, all of it far too
// slow to hold a lock across. So a second request for the same theme almost
// always lands inside it. Reporting a miss sends that request into reserve,
// which recognises the duplicate and resolves it properly.
func (s *embedThemeState) lookup(surface, param string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.resolved[surface][param]
	if key == "" {
		return "", false
	}
	s.touchLocked(surface, param)
	return key, true
}

// reserve claims one of the surface's variant slots for param.
//
// It RESERVES rather than merely checking, because the check and the
// registration cannot be one atomic step: registering renders CSS, which is far
// too slow to hold a lock across. Without a reservation, N concurrent requests
// carrying N distinct themes would each see "under the cap" and each register —
// the cap would bound nothing but the steady state, and a burst is exactly how
// an amplification attack arrives.
//
// The reservation is the map entry itself, with an empty key meaning
// "in flight". A caller that reserves MUST follow with either record or
// release; release exists so a theme that turns out to be malformed does not
// consume a slot permanently.
//
// At the cap it EVICTS the least recently used variant rather than refusing.
// The shell route is unauthenticated by design — it has to be, a frame is
// fetched by a navigation — so an anonymous caller can present as many distinct
// well-formed themes as it likes. Refusing at the cap meant thirty-two requests
// permanently locked a surface out of its own customer's branding for the life
// of the process. Eviction keeps the bound (the point of the cap) without
// letting a stranger decide who gets a slot: the customer's real theme arrives
// on every page load and simply reclaims one.
//
// dup distinguishes "someone else is already resolving exactly this theme" from
// "there is no room". Both used to return the same false, and the caller read
// every false as cap exhaustion — so two visitors opening the same customer's
// page at the same moment on a cold process had one of them silently rendered
// in the app palette instead of the customer's brand.
func (s *embedThemeState) reserve(surface, param string, max int) (ok bool, evicted []string, dup bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resolved == nil {
		s.resolved = map[string]map[string]string{}
	}
	byParam := s.resolved[surface]
	if byParam == nil {
		byParam = map[string]string{}
		s.resolved[surface] = byParam
	}
	if _, taken := byParam[param]; taken {
		// Either resolved (lookup would have hit, so this is a race with a
		// record) or still in flight. Either way this request does not get a
		// second slot — but it is asking for a theme that IS being admitted,
		// which is not the same as being turned away.
		return false, nil, true
	}
	for len(byParam) >= max {
		victim, ok := s.lruLocked(surface, byParam)
		if !ok {
			// Every slot is in flight. Refuse rather than evict a reservation
			// whose owner is mid-render and would then record into a slot it no
			// longer holds.
			return false, evicted, false
		}
		evicted = append(evicted, byParam[victim])
		delete(byParam, victim)
		delete(s.used[surface], victim)
	}
	byParam[param] = ""
	s.pendingLocked(surface, param)
	s.touchLocked(surface, param)
	return true, evicted, false
}

// lruLocked picks the least recently used RESOLVED entry for a surface.
func (s *embedThemeState) lruLocked(surface string, byParam map[string]string) (string, bool) {
	var victim string
	var oldest uint64
	found := false
	for param, key := range byParam {
		if key == "" {
			continue // in flight; not ours to evict
		}
		n := s.used[surface][param]
		if !found || n < oldest {
			victim, oldest, found = param, n, true
		}
	}
	return victim, found
}

// touchLocked stamps a param as most recently used. The counter is monotonic
// per host, so it needs no clock — which matters because the whole point is to
// be cheap on a hot path.
func (s *embedThemeState) touchLocked(surface, param string) {
	if s.used == nil {
		s.used = map[string]map[string]uint64{}
	}
	if s.used[surface] == nil {
		s.used[surface] = map[string]uint64{}
	}
	s.tick++
	s.used[surface][param] = s.tick
}

// record completes a reservation with the variant key that was registered.
func (s *embedThemeState) record(surface, param, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resolved[surface] != nil {
		s.resolved[surface][param] = key
	}
	s.settleLocked(surface, param)
}

// release gives a reservation back. A refused theme leaves NO entry behind in
// EITHER map: caching every rejected parameter would replace one unbounded map
// with another, since the parameter is attacker-chosen.
//
// Both maps, because reserve writes both — resolved directly and used through
// touchLocked. Cleaning only resolved left the used entry permanent and
// invisible: eviction never reaches it (lruLocked iterates resolved), the cap
// never counts it, and the shell route that creates it is unauthenticated, so a
// flood of unusable theme parameters grew the map until the process died while
// every cap and count still read as healthy.
func (s *embedThemeState) release(surface, param string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if byParam := s.resolved[surface]; byParam != nil {
		if byParam[param] == "" {
			delete(byParam, param)
		}
	}
	delete(s.used[surface], param)
	s.settleLocked(surface, param)
}

// mountEmbed registers the embed routes. Called from Mount only when an embed
// host is configured, so an app that does not hand out pieces of itself serves
// no embed surface at all — not even a 404 that confirms the feature exists.
func (ds *UIHost) mountEmbed(r *router.Router) {
	if ds.embedHost == nil {
		return
	}
	r.Get(embedLoaderPath, http.HandlerFunc(ds.handleEmbedLoaderJS))
	r.Get(embedRuntimePath, http.HandlerFunc(ds.handleEmbedRuntimeJS))
	r.Post(embedExchangePath, http.HandlerFunc(ds.handleEmbedExchange))
	r.Post(embedRefreshPath, http.HandlerFunc(ds.handleEmbedRefresh))
	// GET on the API endpoints is 405, not 404. The exchange spends a
	// single-use nonce, so it must never be reachable by anything that fires
	// on navigation, prefetch or an <img> — and a 405 says that plainly
	// instead of looking like a typo.
	r.Get(embedExchangePath, http.HandlerFunc(methodNotAllowed))
	r.Get(embedRefreshPath, http.HandlerFunc(methodNotAllowed))
	r.Get(embedShellPath, http.HandlerFunc(ds.handleEmbedShell))
	r.Get(embedContentPath, http.HandlerFunc(ds.handleEmbedContent))
}

// stripCookies discards every cookie before an embed handler can read one.
//
// Embed routes must not HONOUR a cookie, and this is how that is enforced:
// the credential is removed, so no downstream code — the session reader, the
// CSRF middleware, an app's own handler — can act on it.
//
// It is not a 4xx, and that is deliberate. Normally no cookie arrives at all:
// SameSite is computed against the top-level browsing context, so inside a
// customer's frame the session cookie is never sent. But there is one real case
// where it IS sent — an app at app.acme.com framed by www.acme.com is
// SAME-SITE, so a Strict cookie rides along. Rejecting the request would make
// same-site embedding impossible; discarding the cookie makes it behave exactly
// like the cross-site case, which is the behaviour the whole design assumes.
func stripCookies(r *http.Request) {
	r.Header.Del("Cookie")
}

// applyEmbedFraming relaxes the app's anti-framing headers for one response.
//
// The global security middleware sends X-Frame-Options: DENY, CSP
// frame-ancestors 'none' and Cross-Origin-Resource-Policy: same-origin on every
// response. Those are correct app defaults and they are exactly what an embed
// must not have.
//
// frame-ancestors lists the origins the SHELL resolved for this response, not
// the one that framed us. No Origin header is sent on a navigation GET, so at
// the moment this header is written the server does not know who the framer
// is; the shell decides the list (see embedShellOrigins) and this function
// only writes it. Listing origins does not widen anything: the browser
// enforces against the real ancestor chain, so an eleventh origin still
// cannot frame a surface that lists ten. An empty list fails closed to
// frame-ancestors 'none' (see withFrameAncestors) — never a wildcard.
func applyEmbedFraming(w http.ResponseWriter, origins []string) {
	h := w.Header()
	// X-Frame-Options has no "these specific origins" mode, so it can only be
	// removed. CSP frame-ancestors is the modern, precise control and browsers
	// honour it over XFO — which also means a buffering middleware that
	// re-adds XFO downstream cannot break this.
	h.Del("X-Frame-Options")
	h.Set("Content-Security-Policy", withFrameAncestors(h.Get("Content-Security-Policy"), origins))
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
}

// withFrameAncestors rewrites ONLY the frame-ancestors directive of an existing
// policy, leaving every other directive intact.
//
// Replacing the whole header would be a downgrade dressed as a relaxation: the
// app's policy also carries default-src 'self', script-src, img-src and the
// rest, and an embed document loads scripts and styles like any other page. It
// needs the framing directive widened and nothing else touched.
func withFrameAncestors(policy string, origins []string) string {
	// An empty origin list is the fail-closed shape: the shell's source
	// returned nothing usable, so the directive must read 'none' (the same
	// thing the app default already says) — never a bare "frame-ancestors "
	// with no sources, which is a directive a browser could read oddly, and
	// never a widening to everyone.
	directive := "frame-ancestors 'none'"
	if len(origins) > 0 {
		directive = "frame-ancestors " + strings.Join(origins, " ")
	}
	if strings.TrimSpace(policy) == "" {
		return directive
	}
	parts := strings.Split(policy, ";")
	out := make([]string, 0, len(parts)+1)
	replaced := false
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		name, _, _ := strings.Cut(trimmed, " ")
		if strings.EqualFold(name, "frame-ancestors") {
			out = append(out, directive)
			replaced = true
			continue
		}
		out = append(out, trimmed)
	}
	if !replaced {
		out = append(out, directive)
	}
	return strings.Join(out, "; ")
}

// embedShellOrigins resolves the frame-ancestors origin list for one shell
// response.
//
// With no OriginSource it is the surface's static allowlist, exactly as
// before — an app that never configures a source behaves identically to
// today. With a source it is ONLY the origins of the customer named in the
// request, so the directive no longer publishes the whole customer list and
// one customer's over-large list cannot overflow the response header for
// everyone else.
//
// The customer id is attacker-chosen: it arrives on an unauthenticated
// navigation, so anyone may request another customer's shell and read THAT
// customer's origins. That is a smaller leak than today (the whole list on
// every response) and grants no framing — the browser enforces against the
// real ancestor chain, and a grant stays bound to the origin it was minted
// for. Every failure of the source path (unknown customer, source error,
// over-size list, invalid origin) returns nil, which withFrameAncestors
// writes as frame-ancestors 'none': fail closed, never widen.
func (ds *UIHost) embedShellOrigins(r *http.Request, s *fembed.ResolvedSurface) []string {
	src := ds.embedHost.OriginSource()
	if src == nil {
		return s.AllowedOrigins()
	}
	resolved, err := fembed.ResolveCustomerOrigins(r.Context(), src, s.Name, r.URL.Query().Get("customer"))
	if err != nil {
		slog.Default().Warn("uihost: embed origin source failed closed — serving frame-ancestors 'none'",
			"surface", s.Name, "err", err)
		return nil
	}
	return resolved
}

func (ds *UIHost) handleEmbedLoaderJS(w http.ResponseWriter, r *http.Request) {
	stripCookies(r)
	js, err := runtime.EmbedLoaderJS()
	if err != nil {
		http.Error(w, "embed loader unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// The loader is fetched BY a customer's page, so it is a cross-origin
	// subresource and needs CORP relaxed. It contains no secrets: the nonce
	// lives in the customer's own markup, never in this file.
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	// no-cache, matching /__gofastr/runtime.js. Neither URL is
	// content-addressed, so a long max-age would pin a customer's page to a
	// stale loader — including past a security fix — with no way to bust it.
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(js))
}

// handleEmbedRuntimeJS serves the frame's runtime: the embed fragment
// composition, followed by the app's compiled component actions.
//
// The actions ship in the same response rather than through
// /__gofastr/actions.js because that endpoint is credentialed and a <script
// src> is not a fetch — the runtime's wrapper never sees a script tag, so the
// grant could not ride along and the frame would 401. Without them
// __gofastr.register is never called, handlers stays empty for the life of the
// frame, and the failure is silent in both directions: every data-action-mount
// node renders and never fills (that is every generated entity list and every
// relation <select>), and every data-action click is preventDefault()ed by the
// delegator and then dropped, so the control looks alive and does nothing.
//
// This publishes the app's action names to anyone who can fetch this URL. That
// is the same audience that can already fetch the surface shell — which is
// unauthenticated by necessity, since a frame is loaded by a navigation — and
// which already carries the full component catalog. Configuring an embed host
// is the opt-in.
func (ds *UIHost) handleEmbedRuntimeJS(w http.ResponseWriter, r *http.Request) {
	stripCookies(r)
	js, err := runtime.EmbedJS()
	if err != nil {
		http.Error(w, "embed runtime unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(js))
	// Appended, not concatenated into the composition: actionsToJS emits
	// self-contained IIFEs that read window.__gofastr, so they only need the
	// runtime to have already evaluated.
	if actions := ds.GetActionJS(); actions != "" {
		_, _ = w.Write([]byte("\n"))
		_, _ = w.Write([]byte(actions))
	}
}

// embedExchangeRequest is the frame's handshake payload.
type embedExchangeRequest struct {
	Token  string `json:"token"`
	Origin string `json:"origin"`
}

// embedGrantResponse is what the frame gets back. ExpiresInMS rather than an
// absolute timestamp: the frame schedules a refresh off it, and a clock skew
// between the customer's machine and the server would otherwise make that
// schedule wrong in whichever direction the skew runs.
type embedGrantResponse struct {
	Grant       string `json:"grant"`
	ExpiresInMS int64  `json:"expires_in_ms"`
}

// embedError writes a deliberately uninformative failure.
//
// Every rejection on the exchange path answers the same way. Which check failed
// — bad signature, expired, already used, wrong origin, unknown surface — is an
// oracle: it tells a caller probing with a captured nonce exactly how far they
// got. The server logs the real reason; the client learns only that it failed.
func embedError(w http.ResponseWriter, status int, reason string, err error) {
	if err != nil {
		slog.Default().Debug("uihost: embed request refused", "reason", reason, "err", err)
	}
	http.Error(w, http.StatusText(status), status)
}

func (ds *UIHost) handleEmbedExchange(w http.ResponseWriter, r *http.Request) {
	stripCookies(r)
	if !ds.embedHost.Ready() {
		embedError(w, http.StatusServiceUnavailable, "no signing key", nil)
		return
	}
	// Cap the body: this endpoint is reachable by anything that can send a
	// POST, and a token is a few hundred bytes.
	var req embedExchangeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		embedError(w, http.StatusBadRequest, "malformed body", err)
		return
	}
	res, err := ds.embedHost.Exchange(r.Context(), req.Token, req.Origin)
	if err != nil {
		embedError(w, http.StatusUnauthorized, "exchange refused", err)
		return
	}
	if res.Replay {
		// A replayed exchange is expected once — a prefetched iframe, a
		// double-mounted loader, a refresh — which is why the exchange is
		// idempotent at all. It is also the ONLY externally visible sign of
		// the one failure this feature cannot detect for itself: a nonce
		// baked into a cached customer page, handing one identity to every
		// visitor of that copy. That failure has no error and no wrong
		// status; it renders perfectly, as the wrong person. Say something.
		slog.Warn("embed: nonce exchanged more than once",
			"surface", res.Surface,
			"origin", req.Origin,
			"hint", "one replay is normal (prefetch, refresh); repeated replays mean "+
				"the page carrying the nonce is cached — mint a fresh nonce per render, "+
				"or every visitor of that cached copy acts as the same subject")
	}
	ds.writeEmbedGrant(w, res.Grant, res.Expires.UnixMilli())
}

// handleEmbedRefresh rolls a live grant forward.
//
// Without it a frame someone leaves open — which is what a dashboard embed IS —
// stops working when its grant expires, and every island RPC starts 401ing with
// no way back short of reloading the customer's page. The nonce is long spent
// by then, so the refresh has to be driven by the grant itself.
//
// It cannot extend a grant forever: the grant carries an absolute deadline set
// when it was first issued, and a refresh past that deadline is refused.
func (ds *UIHost) handleEmbedRefresh(w http.ResponseWriter, r *http.Request) {
	stripCookies(r)
	if !ds.embedHost.Ready() {
		embedError(w, http.StatusServiceUnavailable, "no signing key", nil)
		return
	}
	grant, err := ds.embedHost.Refresh(r.Context(), r.Header.Get(embedGrantHeader))
	if err != nil {
		embedError(w, http.StatusUnauthorized, "refresh refused", err)
		return
	}
	ds.writeEmbedGrant(w, grant.Token, grant.Expires.UnixMilli())
}

func (ds *UIHost) writeEmbedGrant(w http.ResponseWriter, grant string, expiresUnixMS int64) {
	w.Header().Set("Content-Type", "application/json")
	// A grant is a bearer credential for one viewer. Nothing may cache it.
	w.Header().Set("Cache-Control", "no-store")
	nowMS := time.Now().UnixMilli()
	ttl := expiresUnixMS - nowMS
	if ttl < 0 {
		ttl = 0
	}
	_ = json.NewEncoder(w).Encode(embedGrantResponse{Grant: grant, ExpiresInMS: ttl})
}

// embedGrant returns the verified grant this request carries, if any.
//
// Used by infrastructure endpoints that normally gate on the session cookie. A
// frame has no cookie by construction, so without this its requests are either
// refused (cross-site) or answered on the strength of an unrelated ambient
// session (same-site) — and "which of those happens" is not something the
// feature should leave to the viewer's browser state.
//
// It returns the grant rather than a bare bool because a caller that accepts an
// embed credential almost always has to narrow what it answers to the grant's
// own surface. A gate that only asks "is this token real" hands every surface's
// answer to whoever holds any surface's token.
func (ds *UIHost) embedGrant(r *http.Request) (fembed.Grant, bool) {
	if ds.embedHost == nil || !ds.embedHost.Ready() {
		return fembed.Grant{}, false
	}
	token := r.Header.Get(embedGrantHeader)
	if token == "" {
		return fembed.Grant{}, false
	}
	g, err := ds.embedHost.VerifyGrant(r.Context(), token)
	if err != nil {
		return fembed.Grant{}, false
	}
	return g, true
}

// embedGrantOK reports whether this request carries a valid embed grant.
func (ds *UIHost) embedGrantOK(r *http.Request) bool {
	_, ok := ds.embedGrant(r)
	return ok
}

// embedSurfacePath returns the app path a grant's surface renders.
//
// Empty when the surface has gone away since the grant was minted — the caller
// must then treat the request as unscoped rather than as unrestricted.
func (ds *UIHost) embedSurfacePath(g fembed.Grant) string {
	if ds.embedHost == nil {
		return ""
	}
	s, ok := ds.embedHost.Lookup(g.Surface)
	if !ok {
		return ""
	}
	return s.Path()
}

// resolveEmbedSurface looks up the surface named in the path.
//
// An unknown name and a name the caller may not reach answer identically (404
// with no body detail): there is no index endpoint, and enumerating which
// surfaces exist should cost the same as guessing.
func (ds *UIHost) resolveEmbedSurface(w http.ResponseWriter, r *http.Request) (*fembed.ResolvedSurface, bool) {
	name := r.PathValue("surface")
	s, ok := ds.embedHost.Lookup(name)
	if !ok {
		// embedError, not http.NotFound: the two write different bodies
		// ("Not Found" vs "404 page not found"), and every 404 on this surface
		// has to be byte-identical or the difference itself answers "does this
		// name exist?".
		embedError(w, http.StatusNotFound, "unknown surface", nil)
		return nil, false
	}
	return s, true
}

// handleEmbedShell serves the frame document.
//
// It is deliberately content-free: a head, one empty root element and the embed
// runtime. The surface's actual content cannot be server-rendered into it,
// because this document is fetched by a navigation and a navigation can carry
// no credential — the frame is anonymous until the postMessage handshake
// completes. boot-embed fetches the content as the granted subject and injects
// it.
//
// The markup here is a document skeleton, not layout: no classes, no CSS, one
// element. Every layout decision inside the frame comes from the design system
// by way of the screen the content endpoint renders under app.EmbedLayout.
func (ds *UIHost) handleEmbedShell(w http.ResponseWriter, r *http.Request) {
	stripCookies(r)
	s, ok := ds.resolveEmbedSurface(w, r)
	if !ok {
		return
	}
	if !ds.embedHost.Ready() {
		// Every other embed handler checks this; the shell did not, so a host
		// wired without an app secret served a framable document for a
		// handshake that could never complete. framework.App.Mount panics in
		// that case, but a host mounted standalone does not go through it.
		embedError(w, http.StatusServiceUnavailable, "no signing key", nil)
		return
	}
	applyEmbedFraming(w, ds.embedShellOrigins(r, s))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The shell carries no identity, so it would be safe to cache — but it
	// also carries the component catalog, whose per-component hashes change on
	// every deploy. A cached shell would demand-load component CSS at hashes
	// the new build no longer serves. no-cache, like every other
	// non-content-addressed asset the host serves.
	w.Header().Set("Cache-Control", "no-cache")

	cfg := map[string]string{
		"surface":  s.Name,
		"content":  "/__gofastr/embed/" + s.Name + "/content",
		"exchange": embedExchangePath,
		"refresh":  embedRefreshPath,
		// The app route the surface renders. Anything inside the frame that
		// asks the server "what belongs on this page" has to name THIS, not the
		// shell's own URL — a widget scoped with .Pages("/reports") is not
		// scoped to /__gofastr/embed/reports.
		"path": s.Path(),
	}
	cfgJSON, _ := json.Marshal(cfg)

	appCSS := "/__gofastr/app.css"
	variantKey := ds.embedThemeKey(s, r.URL.Query().Get("theme"))
	if variantKey != "" {
		appCSS += "?t=" + variantKey
	}
	// Component stylesheets have to resolve under the same theme as app.css, or
	// a component whose StyleFn reads theme values directly keeps the app's
	// palette while everything var()-driven turns the customer's.
	shellTheme := ds.activeTheme()
	if variantKey != "" {
		if t, ok := ds.themeVariant(variantKey); ok {
			shellTheme = t
		}
	}

	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n")
	b.WriteString(`<meta charset="utf-8">` + "\n")
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">` + "\n")
	// An embed is a fragment of an app rendered inside someone else's page. It
	// is never the canonical location of anything, so keep it out of indexes.
	b.WriteString(`<meta name="robots" content="noindex, nofollow">` + "\n")
	fmt.Fprintf(&b, `<meta name="gofastr-embed" content="%s">`+"\n", stdhtml.EscapeString(string(cfgJSON)))
	b.WriteString(`<script src="/__gofastr/color-scheme.js"></script>` + "\n")
	fmt.Fprintf(&b, `<link rel="stylesheet" href="%s">`+"\n", stdhtml.EscapeString(appCSS))
	// The component catalog and the runtime module manifest, as inert
	// <script type="application/json"> blocks — the same shape a normal page
	// ships, and the same reason: the kernel's CSS scanner resolves a
	// data-fui-comp marker to a stylesheet URL through the catalog, and
	// loadModule cache-busts through the manifest.
	//
	// Without the catalog the frame renders every component's MARKUP with none
	// of its CSS: cards lose their surface, grids collapse to a single column,
	// stat cards become bare paragraphs. The failure is invisible to a DOM
	// assertion — the elements are all present — and obvious in a screenshot.
	if catalog := catalogJSONScriptFor(ds, shellTheme, variantKey); catalog != "" {
		b.WriteString(catalog)
		b.WriteByte('\n')
	}
	if manifest := runtimeModuleManifestScript(); manifest != "" {
		b.WriteString(manifest)
		b.WriteByte('\n')
	}
	b.WriteString("</head>\n<body>\n")
	b.WriteString(`<div id="gofastr-embed-root" data-fui-embed-state="loading"></div>` + "\n")
	// The embed runtime carries the app's compiled component actions with it —
	// see handleEmbedRuntimeJS. A separate <script src="/__gofastr/actions.js">
	// cannot work here: a script tag is not a fetch, so the runtime's wrapper
	// never sees it and it would arrive without the grant.
	b.WriteString(`<script src="` + embedRuntimePath + `"></script>` + "\n")
	b.WriteString("</body>\n</html>\n")

	_, _ = w.Write([]byte(b.String()))
}

// embedThemeKey resolves a customer's brand tokens to a registered theme
// variant key, or "" to render under the app theme.
//
// The tokens arrive base64url-encoded in the frame URL rather than through the
// handshake, so the shell can link the right stylesheet in its first response.
// They are not secret — they are the customer's brand colours — and putting
// them in the URL is what avoids rendering the frame in the wrong palette and
// then swapping it.
//
// Three things bound what a customer can do with this:
//   - AllowTokens: only tokens the surface names may be set at all. A surface
//     that names none is not re-themable, which is the default.
//   - style.ApplyTokens: rejects unknown keys and validates values against the
//     colour grammar, so a value cannot escape its declaration.
//   - MaxVariants: past the cap the surface renders under the app theme. Every
//     distinct theme is a fresh render and a component-CSS cache miss, so an
//     uncapped registry is a cheap amplification.
//
// Every failure falls back to the app theme rather than erroring: a customer
// with a stale or malformed brand config gets the app's own look, not a broken
// embed.
func (ds *UIHost) embedThemeKey(s *fembed.ResolvedSurface, encoded string) string {
	if encoded == "" || len(s.Theme.AllowTokens) == 0 {
		return ""
	}
	// Bound the parameter BEFORE it can become a map key. resolveEmbedTheme
	// applies the same limit, but it runs after the reservation is recorded, so
	// an oversize value was already stored — and the only bound on its length
	// there was the server's request-line limit, three orders of magnitude
	// larger than this one.
	if len(encoded) > maxEmbedThemeParam {
		return ""
	}
	// Resolved once per distinct parameter; a repeat is one map read.
	if key, found := ds.embedThemes.lookup(s.Name, encoded); found {
		return key
	}
	max := s.Theme.MaxVariants
	if max <= 0 {
		max = fembed.DefaultMaxThemeVariants
	}
	admitted, evicted, dup := ds.embedThemes.reserve(s.Name, encoded, max)
	for _, key := range evicted {
		// Drop the underlying registration too, or eviction trades a lockout
		// for unbounded growth in the variant registry.
		ds.ReleaseThemeVariant(key)
	}
	if dup {
		// Another request is resolving this exact theme right now. Wait for it
		// rather than resolving a second copy.
		//
		// Resolving independently and releasing the extra registration looked
		// equivalent — the key is a content address, so both land on the same
		// one — but it is not: when the duplicate got there FIRST, its register
		// took the refcount 0→1 and its release took it 1→0, deleting the
		// variant before the owner had registered anything. The duplicate then
		// returned a key nothing held, and the frame rendered under the app
		// palette with a ?t= pointing at nothing.
		//
		// The wait is bounded because the owner may fail, and a request that
		// waits forever on someone else's render is worse than one that renders
		// unthemed. Falling through to "" on timeout is what an unknown hash
		// already does.
		if key, ok := ds.embedThemes.waitFor(s.Name, encoded, embedThemeWait); ok && key != "" {
			return key
		}
		return ""
	}
	if !admitted {
		slog.Default().Warn("uihost: embed theme variants all in flight — rendering under the app theme",
			"surface", s.Name, "max", max)
		return ""
	}
	// From here every failure path must give the slot back, or malformed input
	// would exhaust the cap and lock a customer out of their own branding.
	key, ok := ds.resolveEmbedTheme(s, encoded)
	if !ok {
		ds.embedThemes.release(s.Name, encoded)
		return ""
	}
	ds.embedThemes.record(s.Name, encoded, key)
	return key
}

// resolveEmbedTheme decodes the customer's tokens, filters them against the
// surface's allowlist, and registers the resulting theme. ok=false means the
// parameter was unusable and the surface should render under the app theme.
func (ds *UIHost) resolveEmbedTheme(s *fembed.ResolvedSurface, encoded string) (string, bool) {
	// Bound the ENCODED input first. Decoding a megabyte query string just to
	// reject it allocates three quarters of a megabyte on an unauthenticated
	// path. embedThemeKey applies the same limit before the parameter can
	// become a map key; this one keeps the function safe for any caller.
	if len(encoded) > maxEmbedThemeParam {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(raw) > 4<<10 {
		return "", false
	}
	var tokens map[string]string
	if err := json.Unmarshal(raw, &tokens); err != nil || len(tokens) == 0 {
		return "", false
	}
	allowed := make(map[string]struct{}, len(s.Theme.AllowTokens))
	for _, t := range s.Theme.AllowTokens {
		allowed[t] = struct{}{}
	}
	filtered := make(map[string]string, len(tokens))
	for k, v := range tokens {
		if _, ok := allowed[k]; !ok {
			// A token the surface did not allow is dropped, not an error: the
			// customer's config outliving a narrowed allowlist should degrade
			// to the app's value for that token, not break the embed.
			continue
		}
		filtered[k] = v
	}
	if len(filtered) == 0 {
		return "", false
	}
	themed, err := style.ApplyTokens(ds.activeTheme(), filtered)
	if err != nil {
		return "", false
	}
	return ds.RegisterThemeVariant(themed), true
}

// handleEmbedContent renders the surface's screen as the granted subject.
//
// This is the only embed route that carries identity, and the grant header is
// the only place identity can come from: no cookie reaches here (stripCookies),
// and none would have been sent anyway.
func (ds *UIHost) handleEmbedContent(w http.ResponseWriter, r *http.Request) {
	stripCookies(r)
	s, ok := ds.resolveEmbedSurface(w, r)
	if !ok {
		return
	}
	if !ds.embedHost.Ready() {
		embedError(w, http.StatusServiceUnavailable, "no signing key", nil)
		return
	}
	grant, err := ds.embedHost.VerifyGrant(r.Context(), r.Header.Get(embedGrantHeader))
	if err != nil {
		embedError(w, http.StatusUnauthorized, "grant refused", err)
		return
	}
	// A grant for surface A must not render surface B. The grant names its
	// surface and the URL names one too; they have to agree.
	//
	// The answer is 404, matching an unknown name exactly. A 403 here would
	// have told any grant holder which surface names are real — the same
	// enumeration the absence of an index endpoint is meant to prevent.
	if grant.Surface != s.Name {
		embedError(w, http.StatusNotFound, "grant is for another surface", nil)
		return
	}

	ctx := app.WithRequest(r.Context(), r)
	ctx = store.WithValues(ctx)
	// Install the grant itself, so embed.GrantFromContext answers on the FIRST
	// render and not only on the island RPCs that come after it.
	//
	// The two directions fail differently and both are wrong. A screen writing
	// `if _, embedded := GrantFromContext(ctx); !embedded { firstPartyOnly() }`
	// fails OPEN — ok=false is documented as "an ordinary first-party request",
	// so the frame renders exactly what it must not. A screen gating on
	// HasScope fails closed, but inconsistently: the same component's island
	// goes through Host.Middleware, which does install the grant, so the section
	// appears on first paint and vanishes on the first swap.
	ctx = fembed.WithGrant(ctx, grant)
	// Clear any identity the app's own middleware installed before this
	// handler ran.
	//
	// stripCookies runs here, inside the handler — but session middleware runs
	// BEFORE it, on the app's router, and this package does not control that
	// ordering. So a same-site framing (app.acme.com inside www.acme.com, where
	// a Strict cookie really is sent) could otherwise reach this point with a
	// user already on the context, and any path below that does not overwrite
	// it would render the surface as the cookie's user. Reset first; the grant
	// is the only identity an embedded surface may have.
	ctx = handler.SetUser(ctx, nil)
	// The TENANT too, and for the same reason.
	//
	// Clearing the user and leaving the tenant is the worst of both: the
	// surface then renders as the GRANT's subject scoped to the COOKIE
	// visitor's tenant, which is the pairing Host.Middleware names as tenant
	// isolation off. The island path refuses that combination outright; this
	// route was serving it, on the first paint.
	ctx = handler.SetTenant(ctx, nil)
	ctx = tenant.SetTenantID(ctx, "")

	// Install the granted identity so the screen's policies and its
	// ContextComponents see a logged-in user exactly as they would on a
	// first-party page. Without a resolver the surface renders anonymously,
	// which is the right shape for a public pricing table or status widget.
	//
	// Tenant first: the subject lookup is then tenant-scoped, matching the
	// order Host.Middleware uses, so a subject that does not belong to the
	// resolved tenant fails closed here rather than rendering.
	if resolveTenant := ds.embedHost.TenantResolver(); resolveTenant != nil && grant.Subject != "" {
		tid, err := resolveTenant(ctx, grant.Subject)
		if err != nil {
			embedError(w, http.StatusForbidden, "tenant did not resolve", err)
			return
		}
		if tid != "" {
			ctx = handler.SetTenant(ctx, tid)
			ctx = tenant.SetTenantID(ctx, tid)
		}
	}
	if resolve := ds.embedHost.Resolver(); resolve != nil && grant.Subject != "" {
		user, err := resolve(ctx, grant.Subject)
		if err != nil {
			// The subject no longer resolves (deleted, disabled, store
			// outage). Fail closed: rendering anonymously here would silently
			// downgrade an authenticated embed into a public one.
			embedError(w, http.StatusForbidden, "subject did not resolve", err)
			return
		}
		// isNilValue, not user != nil: a resolver written as
		// func(...) (*User, error) returning a nil *User yields a non-nil
		// interface wrapping nil, and installing it makes every "is a user
		// present" gate downstream report authenticated for a subject that
		// does not exist. Host.Middleware has guarded this since v0.49.0; this
		// route was still using the bare comparison.
		if !fembed.IsNilValue(user) {
			ctx = handler.SetUser(ctx, user)
		}
	}

	res, err := ds.App.RenderPartialResult(ctx, s.Path())
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch res.Kind {
	case app.DecisionRedirect:
		// A screen policy wants the viewer elsewhere. There is nowhere to send
		// them from inside a frame on a stranger's site, and following the
		// redirect would render a DIFFERENT screen than the surface declares.
		embedError(w, http.StatusForbidden, "screen policy redirected", nil)
		return
	case app.DecisionBlock:
		status := res.Status
		if status == 0 {
			status = http.StatusForbidden
		}
		embedError(w, status, "screen policy blocked", nil)
		return
	}

	applyEmbedFraming(w, s.AllowedOrigins())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Rendered for one subject. Never cache, never share.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(app.EmbedLayout().Wrap(res.HTML)))
}
