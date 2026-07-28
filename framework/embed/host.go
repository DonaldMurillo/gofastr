package embed

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// Default token lifetimes.
//
// The nonce window is short because the nonce is rendered into a page the app
// does not control and is spent by the very next request the browser makes. A
// minute absorbs a slow page load and a prefetch; it does not absorb a nonce
// pasted into a template and served for a week — which is the failure this
// design exists to prevent.
//
// The grant window is what bounds how long a frame keeps working after its
// nonce is spent. It refreshes while the frame lives, so a short window costs
// one background request rather than a broken embed.
const (
	DefaultNonceTTL = time.Minute
	DefaultGrantTTL = 15 * time.Minute
	// DefaultGrantMaxAge is how long a frame may keep refreshing before the
	// customer's page has to hand it a fresh nonce. It bounds an otherwise
	// immortal credential: a dashboard embed left open in a tab for a week
	// should not still be acting as its viewer.
	DefaultGrantMaxAge = 12 * time.Hour
)

// DefaultMaxThemeVariants caps how many distinct customer themes one surface
// may register.
//
// Component CSS is content-addressed by theme, so every distinct theme is a
// cache miss plus a fresh render. Without a cap, a customer that varies a token
// per page view turns the theme registry into an unbounded map and the CSS
// cache into a miss generator.
const DefaultMaxThemeVariants = 32

// ThemeConfig restricts how far a customer may re-theme a surface.
type ThemeConfig struct {
	// AllowTokens names the style tokens a customer may override, using the
	// same names style.ThemeToTokens emits. Empty means the surface is not
	// re-themable at all — the safe default, since a token allowlist is the
	// only thing standing between "brand colour" and "restyle the confirm
	// button to look like the cancel button".
	AllowTokens []string
	// MaxVariants caps distinct registered themes for this surface.
	// Zero uses DefaultMaxThemeVariants.
	MaxVariants int
}

// Screen is the minimal view of a core-ui/app.Screen this package needs: the
// app route the surface renders. *app.Screen satisfies it structurally, so
// this package never imports the UI layer — battery/auth imports this package,
// and dragging core-ui in would be a layering regression.
//
// A Surface carries the screen value rather than a path string so the link
// from a surface to the component tree it renders is a Go value — followable
// by a human, a static analyzer, and the boot-time server-action walk —
// instead of a string resolved against a route table.
type Screen interface {
	// RoutePath is the app route the screen is mounted at, e.g. "/reports".
	RoutePath() string
}

// Surface is one embeddable piece of the app.
type Surface struct {
	// Name is the id a customer references in the embed snippet. It appears in
	// a URL, so it is restricted to lowercase letters, digits and dashes.
	Name string
	// Screen is the app screen rendered inside the frame. Required: a surface
	// renders a screen, not a path string, so the framework can follow a
	// surface to the component tree it renders without resolving strings.
	//
	// *app.Screen (core-ui/app) satisfies the Screen interface above. Pass the
	// same *app.Screen value you register with App.RegisterScreen to the
	// surface, so the link is a value identity rather than a re-typed path:
	//
	//	reports := app.NewScreen("/reports", &ReportsScreen{})
	//	application.RegisterScreen(reports, app.EmbedLayout())
	//	embed.Surface{Name: "reports", Screen: reports, Origins: ...}
	//
	// An island-only embed is a screen whose body is that island: the
	// chrome-less embed layout emits no header, nav or footer, so a
	// single-island screen renders as exactly that island and nothing else.
	// There is deliberately no second render path for islands — one path means
	// one set of security decisions.
	Screen Screen
	// Origins lists the exact origins allowed to frame this surface. Required;
	// there is no wildcard and no "allow any" spelling.
	Origins []string
	// Scopes is the capability set a nonce for this surface may carry.
	// MintNonce may narrow it but never widen it.
	Scopes []string
	// Theme restricts customer re-theming. Zero value means not re-themable.
	Theme ThemeConfig
	// Reach lists ADDITIONAL path prefixes a grant for this surface may reach,
	// beyond the surface's own Path subtree and the runtime's /__gofastr/*
	// endpoints. Anything else answers 403.
	//
	// The default is closed because a grant is delegated authority that lives
	// in a page the app does not control, and the alternative — reach
	// everything the subject can reach, unless the author remembers to gate it
	// — lost every time it was tried: the framework itself mounts /mcp,
	// {auth}/tokens and /admin/*, so "the author will gate it" was never a
	// property anybody could hold.
	//
	// A surface whose form posts to /api/orders declares that here:
	//
	//	Reach: []string{"/api/orders"}
	//
	// Prefixes match on segment boundaries, so "/api/orders" admits
	// "/api/orders" and "/api/orders/42" but not "/api/orders-archive".
	// Reach is per-surface: a grant for one surface never inherits another's.
	Reach []string
}

// SubjectResolver turns a grant's subject id into the value installed as the
// request's current user.
//
// It exists so this package can stay out of the auth battery: the app supplies
// the lookup (usually the auth manager's FindByID) and gets whatever identity
// type its screens already expect. Returning an error fails the request closed.
type SubjectResolver func(ctx context.Context, subject string) (any, error)

// TenantResolver turns a grant's subject into the tenant id its requests run
// under. Returning an error fails the request closed.
type TenantResolver func(ctx context.Context, subject string) (string, error)

// Config declares an app's embeddable surfaces.
type Config struct {
	// Surfaces is the closed set of embeddable surfaces. A name that is not
	// here does not exist — there is no index endpoint and unknown names 404
	// identically to unauthorized ones.
	Surfaces []Surface
	// BurnStore records spent nonces. Required. Use NewSQLBurnStore for
	// anything running more than one replica.
	BurnStore BurnStore
	// Resolve maps a grant subject to the current user. Optional: without it
	// an embed renders anonymously, which is the right shape for a public
	// pricing table or status widget.
	Resolve SubjectResolver
	// ResolveTenant maps a grant subject to its tenant id. Optional.
	//
	// Middleware clears the tenant along with every other ambient identity
	// value, because inheriting the COOKIE user's tenant is a cross-tenant
	// read. Without this hook it then had no way to install the right one, so
	// a multi-tenant entity behind an embed simply errored — the app's only
	// recourse was undocumented middleware of its own.
	//
	// The tenant comes from the app's lookup ON THE GRANT'S SUBJECT, never
	// from the request, so a stolen grant cannot choose its own tenant. That
	// is also why this is a resolver rather than a claim inside the token: a
	// claim has to be correct at mint time, in a credential living in a page
	// the app does not control, and a wrong one is a cross-tenant read.
	ResolveTenant TenantResolver
	// NonceTTL / GrantTTL / GrantMaxAge override the defaults above.
	NonceTTL    time.Duration
	GrantTTL    time.Duration
	GrantMaxAge time.Duration
	// OriginSource optionally supplies a surface's allowed origins per
	// customer at request time. When set, the embed shell serves only the
	// origins of the customer named in the request instead of the whole
	// static allowlist — see [OriginSource] and the Origins section of the
	// embed docs. Leave it nil to behave exactly as today (static
	// Surface.Origins on every shell response).
	OriginSource OriginSource
}

// Host serves an app's embeddable surfaces. Construct with [New].
type Host struct {
	surfaces      map[string]*ResolvedSurface
	names         []string
	store         BurnStore
	resolve       SubjectResolver
	resolveTenant TenantResolver
	originSource  OriginSource
	nonceTTL      time.Duration
	grantTTL      time.Duration
	grantMaxAge   time.Duration

	nonceKey []byte
	grantKey []byte

	// reserved is this host's effective reserved-prefix list: the package
	// defaults plus whatever the framework registered for the batteries this
	// app actually mounted. Per-host rather than package-global so one app's
	// mount cannot change another's validation, which matters most in tests.
	reserved []string
}

// ResolvedSurface is a declared Surface with its origins normalized once at
// boot, so no request path ever re-parses an allowlist.
//
// path is the screen's route resolved and normalized once at boot. MayReach
// and every other path comparison read it from here, so swapping Surface to
// carry a screen instead of a path string left the request-time matching logic
// unchanged.
type ResolvedSurface struct {
	Surface
	path    string
	origins *originSet
	scopes  map[string]struct{}
}

// Path returns the validated, normalized app route a grant for this surface
// may reach as its own subtree. It is resolved once at boot from the surface's
// screen. Callers that compared the old Surface.Path string read this instead.
func (s *ResolvedSurface) Path() string { return s.path }

// validSurfaceName mirrors the module-name rule in core-ui/runtime: the name
// lands in a URL path segment, so anything that could be read as traversal or
// as a different path is rejected outright rather than escaped later.
func validSurfaceName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
		default:
			return false
		}
	}
	return true
}

// New validates a config and returns the host.
//
// Every failure here is a wiring mistake that would otherwise surface as a
// silently-open or silently-broken embed, so all of them are errors at boot.
func New(cfg Config) (*Host, error) {
	if len(cfg.Surfaces) == 0 {
		return nil, errors.New("embed: at least one Surface is required")
	}
	if cfg.BurnStore == nil {
		return nil, errors.New("embed: a BurnStore is required — single-use nonces cannot be enforced without one")
	}
	h := &Host{
		surfaces:      make(map[string]*ResolvedSurface, len(cfg.Surfaces)),
		store:         cfg.BurnStore,
		resolve:       cfg.Resolve,
		resolveTenant: cfg.ResolveTenant,
		originSource:  cfg.OriginSource,
		nonceTTL:      cfg.NonceTTL,
		grantTTL:      cfg.GrantTTL,
		grantMaxAge:   cfg.GrantMaxAge,
		reserved:      slices.Clone(reservedPrefixes),
	}
	if h.nonceTTL <= 0 {
		h.nonceTTL = DefaultNonceTTL
	}
	if h.grantTTL <= 0 {
		h.grantTTL = DefaultGrantTTL
	}
	if h.grantMaxAge <= 0 {
		h.grantMaxAge = DefaultGrantMaxAge
	}
	if h.grantMaxAge <= h.grantTTL {
		// A max age at or below the per-grant TTL means every grant is born at
		// its deadline and refresh can never succeed: Exchange clamps the
		// expiry to min(now+TTL, deadline), and every Refresh clamps back to the
		// same deadline, so the loop makes no forward progress. Say so at boot
		// rather than shipping an embed that dies after one window.
		//
		// Equality is the interesting half — it produces exactly the failure
		// this guard names while reading like a legal configuration.
		return nil, fmt.Errorf("embed: GrantMaxAge (%s) must be greater than GrantTTL (%s)", h.grantMaxAge, h.grantTTL)
	}
	for _, s := range cfg.Surfaces {
		if !validSurfaceName(s.Name) {
			return nil, fmt.Errorf("embed: surface name %q must be lowercase letters, digits and dashes", s.Name)
		}
		if _, dup := h.surfaces[s.Name]; dup {
			return nil, fmt.Errorf("embed: surface %q declared twice", s.Name)
		}
		// A surface renders a screen, not a path string. A nil screen is a
		// boot error — there is no surface without the component tree it
		// renders, and a path string with no screen is the gap this change
		// exists to close (neither a human nor a static analyzer could follow
		// it to what actually renders).
		if s.Screen == nil {
			return nil, fmt.Errorf("embed: surface %q: Screen is required — a surface renders a screen, not a path string", s.Name)
		}
		route := s.Screen.RoutePath()
		if !strings.HasPrefix(route, "/") {
			return nil, fmt.Errorf("embed: surface %q screen route must be an absolute app route, got %q", s.Name, route)
		}
		// The screen's route gets the SAME validation as Reach, and for a
		// stronger reason.
		//
		// MayReach checks the surface's own subtree first, so the route grants
		// everything a Reach entry would — and it is the value an author binds
		// a surface with. Validating one and not the other meant
		// Reach: []string{"/auth"} failed at boot with a clear error while a
		// screen mounted at "/auth" booted clean and handed every grant the
		// entire auth battery. Normalising here also fixes the trailing-slash
		// footgun: a screen at "/reports/" matched neither /reports nor
		// /reports/42, so the surface rendered and then 403'd its own islands.
		norm, err := normalizeReach(s.Name, "Path", route, h.reserved)
		if err != nil {
			return nil, err
		}
		origins, err := newOriginSet(s.Origins)
		if err != nil {
			return nil, fmt.Errorf("embed: surface %q: %w", s.Name, err)
		}
		scopes := make(map[string]struct{}, len(s.Scopes))
		for _, sc := range s.Scopes {
			scopes[sc] = struct{}{}
		}
		// Validate Reach at boot. A prefix that would hand an embed the whole
		// app, or one of the routes the framework mounts itself, is a
		// configuration that cannot be right — say so here rather than serving
		// it.
		if len(s.Reach) > 0 {
			cleaned := make([]string, 0, len(s.Reach))
			for _, r := range s.Reach {
				norm, err := normalizeReach(s.Name, "Reach entry", r, h.reserved)
				if err != nil {
					return nil, err
				}
				cleaned = append(cleaned, norm)
			}
			s.Reach = cleaned
		}
		h.surfaces[s.Name] = &ResolvedSurface{Surface: s, path: norm, origins: origins, scopes: scopes}
		h.names = append(h.names, s.Name)
	}
	sort.Strings(h.names)
	return h, nil
}

// SetKeys installs the HMAC keys for nonces and grants. The framework calls
// this at mount time with keys HKDF-derived from the app secret; tests and
// standalone uses may call it directly.
func (h *Host) SetKeys(nonceKey, grantKey []byte) {
	h.nonceKey = nonceKey
	h.grantKey = grantKey
}

// Ready reports whether the host has signing keys. A host without keys cannot
// mint or verify anything and its routes answer 503 rather than pretending.
func (h *Host) Ready() bool { return len(h.nonceKey) > 0 && len(h.grantKey) > 0 }

// Names returns the declared surface names, sorted. For diagnostics and tests
// only — it is never served, because an index endpoint would turn "which
// surfaces exist" into a public fact.
func (h *Host) Names() []string {
	out := make([]string, len(h.names))
	copy(out, h.names)
	return out
}

// Lookup returns a declared surface.
func (h *Host) Lookup(name string) (*ResolvedSurface, bool) {
	s, ok := h.surfaces[name]
	return s, ok
}

// AllowedOrigins returns the surface's normalized allowlist, in declaration
// order. This is the list that goes into the frame-ancestors directive.
func (s *ResolvedSurface) AllowedOrigins() []string { return s.origins.List() }

// AllowsOrigin reports whether candidate may frame this surface.
func (s *ResolvedSurface) AllowsOrigin(candidate string) bool { return s.origins.Has(candidate) }

// GrantTTL exposes the configured grant lifetime.
func (h *Host) GrantTTL() time.Duration { return h.grantTTL }

// TenantResolver returns the configured tenant resolver, or nil.
//
// The embed content route resolves identity itself — it builds a fresh context
// rather than passing through Middleware — so it needs this the same way it
// needs Resolver(). Without it, ResolveTenant reached every island RPC and not
// the first paint, so a multi-tenant surface rendered untenanted and then
// tenanted one swap later.
func (h *Host) TenantResolver() TenantResolver { return h.resolveTenant }

// Resolver returns the configured subject resolver, or nil.
func (h *Host) Resolver() SubjectResolver { return h.resolve }

// OriginSource returns the configured per-customer origin source, or nil when
// the app serves only the static Surface.Origins allowlist. The embed shell
// uses this to decide whether to resolve a per-customer frame-ancestors
// directive; nil means "behave exactly as today".
func (h *Host) OriginSource() OriginSource { return h.originSource }

// MintNonce signs a single-use handshake nonce for one viewer of one surface on
// one customer origin.
//
// Call it from the app's own backend while rendering the customer's page — that
// is where the app knows WHICH viewer this embed is for. The returned string is
// safe to place in HTML: it is single-use, expires in a minute, and binds the
// origin it was minted for.
//
// scopes narrows the surface's declared scopes. Passing nil grants the
// surface's full set; passing a scope the surface does not declare is an error
// rather than a silent drop, because a silent drop makes an over-broad call
// site look like it worked.

// originAllowed reports whether origin may frame this surface, consulting the
// static allowlist first and the OriginSource second.
//
// Static first is deliberate: it is a map lookup, it covers every app that
// never configures a source, and it keeps the source off the hot path for
// origins the operator listed at boot. The source is asked only about origins
// the static list does not know — which is exactly the set it exists to serve.
//
// A source error fails CLOSED. The alternative reading, "the store is down so
// let it through", would turn an outage into an open framing policy.
//
// Effect on removal: an origin dropped from the source stops verifying on the
// next request, the same as one dropped from Surface.Origins. Nothing waits
// for a grant to expire.
func (h *Host) originAllowed(ctx context.Context, s *ResolvedSurface, surfaceName, origin string) (bool, error) {
	if s.origins.Has(origin) {
		return true, nil
	}
	if h.originSource == nil {
		return false, nil
	}
	normalized, err := NormalizeOrigin(origin)
	if err != nil {
		return false, nil
	}
	ok, err := h.originSource.Allows(ctx, surfaceName, normalized)
	if err != nil {
		return false, fmt.Errorf("embed: origin source failed for surface %q: %w", surfaceName, err)
	}
	return ok, nil
}

func (h *Host) MintNonce(ctx context.Context, surfaceName, subject, origin string, scopes []string) (string, error) {
	if !h.Ready() {
		return "", errors.New("embed: no signing key — set an app secret (WithSecret or GOFASTR_SECRET)")
	}
	s, ok := h.surfaces[surfaceName]
	if !ok {
		return "", fmt.Errorf("embed: unknown surface %q", surfaceName)
	}
	allowed, err := h.originAllowed(ctx, s, surfaceName, origin)
	if err != nil {
		return "", err
	}
	if !allowed {
		return "", fmt.Errorf("embed: origin %q is not allowed to frame surface %q", origin, surfaceName)
	}
	granted := s.Scopes
	if scopes != nil {
		for _, sc := range scopes {
			if _, ok := s.scopes[sc]; !ok {
				return "", fmt.Errorf("embed: surface %q does not declare scope %q", surfaceName, sc)
			}
		}
		granted = scopes
	}
	normalized, err := NormalizeOrigin(origin)
	if err != nil {
		return "", err
	}
	return MintNonce(h.nonceKey, surfaceName, subject, normalized, granted, h.nonceTTL, time.Now())
}

// ExchangeResult reports what an exchange produced.
type ExchangeResult struct {
	// Grant is the frame credential.
	Grant string
	// Expires is when Grant stops verifying.
	Expires time.Time
	// Replay is true when this exchange returned a previously issued grant
	// rather than minting a new one. The caller answers a replay exactly as it
	// answers a first exchange — that is the whole point of idempotency — but
	// it should SAY something, because a run of replays is the only visible
	// symptom of a nonce baked into a cached customer page, which serves one
	// identity to every visitor of that copy.
	Replay bool
	// Surface names the surface the nonce was minted for, so a caller
	// reporting a replay can say which one.
	Surface string
}

// ErrSpent means the nonce was already exchanged and its grant window closed.
var ErrSpent = errors.New("embed: nonce already used")

// Exchange verifies a handshake nonce, burns it, and returns the frame's grant.
//
// The order is deliberate: verify first (so an unsigned string never reaches
// the store and cannot be used to probe or fill it), then burn, then return.
// The grant is minted BEFORE the burn because the burn stores it — that is what
// makes a repeat exchange idempotent instead of a failure.
//
// framedOrigin is the browser-attested ancestor origin the frame reports.
// It is checked against the nonce when present, but it is NOT the control that
// stops another site from framing the surface: a non-browser caller can send
// anything here. The load-bearing control is the CSP frame-ancestors directive
// on the embed document, which the browser enforces against the real ancestor
// chain. This check is defence in depth and a much better error message.
func (h *Host) Exchange(ctx context.Context, nonceToken, framedOrigin string) (ExchangeResult, error) {
	if !h.Ready() {
		return ExchangeResult{}, errors.New("embed: no signing key")
	}
	now := time.Now()
	n, err := VerifyNonce(h.nonceKey, nonceToken, now)
	if err != nil {
		return ExchangeResult{}, err
	}
	s, ok := h.surfaces[n.Surface]
	if !ok {
		// The surface was removed after the nonce was minted. Fail closed.
		return ExchangeResult{}, fmt.Errorf("embed: unknown surface %q", n.Surface)
	}
	allowedOrigin, err := h.originAllowed(ctx, s, n.Surface, n.Origin)
	if err != nil {
		return ExchangeResult{}, err
	}
	if !allowedOrigin {
		// The origin was removed from the allowlist after the nonce was minted.
		return ExchangeResult{}, fmt.Errorf("embed: origin %q is no longer allowed to frame surface %q", n.Origin, n.Surface)
	}
	// The frame must report the ancestor origin the browser attested, and it
	// must match. Required, not optional: an empty value used to skip the
	// comparison entirely, which meant the one caller the check could plausibly
	// catch — a direct POST that never went through a browser — was also the
	// one caller it let through.
	//
	// Even required, this is corroboration and not the control. A non-browser
	// caller can send whatever it likes here; what actually stops another site
	// from framing the surface is the CSP frame-ancestors directive on the
	// embed document, which the browser enforces against the real chain.
	reported, err := NormalizeOrigin(framedOrigin)
	if err != nil || reported != n.Origin {
		return ExchangeResult{}, fmt.Errorf("embed: nonce was minted for %q but the frame reports %q", n.Origin, framedOrigin)
	}

	// The token encodes its expiry in whole seconds, so report the same value
	// the frame will actually verify against. Reporting sub-second precision
	// here made a replay's expiry differ from the original by a fraction of a
	// second and the two paths disagree about when to refresh.
	deadline := time.Unix(now.Add(h.grantMaxAge).Unix(), 0)
	expires := time.Unix(now.Add(h.grantTTL).Unix(), 0)
	if expires.After(deadline) {
		expires = deadline
	}
	grant, err := MintGrant(h.grantKey, n, h.grantTTL, deadline, now)
	if err != nil {
		return ExchangeResult{}, err
	}
	// The burn row must outlive the NONCE, not the grant.
	//
	// Prune deletes rows past their stored expiry, and the nonce's lifetime is
	// an independent knob from the grant's. Storing the grant's expiry means a
	// config with NonceTTL longer than GrantTTL has a window where Prune has
	// deleted the evidence but VerifyNonce still passes — the next exchange's
	// INSERT wins and mints a SECOND, distinct grant. One nonce, two
	// identities. Keeping the row until the nonce itself is dead closes that
	// window for every TTL combination rather than forbidding some of them.
	//
	// A replay arriving after the grant expired but before the nonce does still
	// answers ErrSpent: the replay branch below re-verifies the stored grant.
	retain := expires
	if n.Expires.After(retain) {
		retain = n.Expires
	}
	issued, replay, err := h.store.Burn(ctx, n.ID, grant, retain)
	if err != nil {
		return ExchangeResult{}, err
	}
	if issued == "" {
		return ExchangeResult{}, ErrSpent
	}
	if replay {
		// Re-derive the expiry from the STORED grant rather than reusing the
		// one we just computed: a replay returns the original grant, and
		// reporting a fresh expiry for it would tell the frame to stop
		// refreshing well after the credential actually died.
		g, err := VerifyGrant(h.grantKey, issued, now)
		if err != nil {
			return ExchangeResult{}, ErrSpent
		}
		return ExchangeResult{Grant: issued, Expires: g.Expires, Replay: true, Surface: n.Surface}, nil
	}
	return ExchangeResult{Grant: grant, Expires: expires, Replay: false, Surface: n.Surface}, nil
}

// VerifyGrant checks a grant against the host's key and confirms the surface it
// names still exists and still allows the origin it was minted for.
func (h *Host) VerifyGrant(ctx context.Context, token string) (Grant, error) {
	if !h.Ready() {
		return Grant{}, errors.New("embed: no signing key")
	}
	g, err := VerifyGrant(h.grantKey, token, time.Now())
	if err != nil {
		return Grant{}, err
	}
	s, ok := h.surfaces[g.Surface]
	if !ok {
		return Grant{}, fmt.Errorf("embed: unknown surface %q", g.Surface)
	}
	allowedOrigin, err := h.originAllowed(ctx, s, g.Surface, g.Origin)
	if err != nil {
		return Grant{}, err
	}
	if !allowedOrigin {
		return Grant{}, fmt.Errorf("embed: origin %q is no longer allowed to frame surface %q", g.Origin, g.Surface)
	}
	// Intersect with the surface's CURRENT scopes.
	//
	// Removing a surface and de-listing an origin both take effect for grants
	// already in flight; narrowing Surface.Scopes did not, so a grant kept a
	// scope the app had revoked for up to GrantMaxAge — twelve hours by
	// default, refreshed the whole way. All three are config the operator
	// changed to take something away; all three now do.
	g.Scopes = intersectScopes(g.Scopes, s.scopes)
	return g, nil
}

// intersectScopes drops any scope the surface no longer declares, preserving
// the grant's order.
func intersectScopes(granted []string, declared map[string]struct{}) []string {
	if len(granted) == 0 {
		return granted
	}
	out := make([]string, 0, len(granted))
	for _, sc := range granted {
		if _, ok := declared[sc]; ok {
			out = append(out, sc)
		}
	}
	return out
}

// RefreshedGrant is a rolled-forward frame credential.
type RefreshedGrant struct {
	Token   string
	Expires time.Time
}

// ErrGrantExhausted means the credential reached its absolute deadline. The
// frame cannot recover on its own — the customer's page must load a new nonce.
var ErrGrantExhausted = errors.New("embed: grant reached its absolute deadline")

// Refresh rolls a live grant forward without spending another nonce.
//
// A frame someone leaves open outlives any sane grant window, and the nonce
// that created it is long burned, so the credential has to renew itself. What
// stops that from being an immortal credential is the deadline the grant has
// carried since it was issued: Refresh moves the expiry, never the deadline,
// and refuses once the deadline passes.
func (h *Host) Refresh(ctx context.Context, token string) (RefreshedGrant, error) {
	g, err := h.VerifyGrant(ctx, token)
	if err != nil {
		return RefreshedGrant{}, err
	}
	now := time.Now()
	if !now.Before(g.Deadline) {
		return RefreshedGrant{}, ErrGrantExhausted
	}
	next, err := MintGrant(h.grantKey, Nonce{
		Surface: g.Surface,
		Subject: g.Subject,
		Scopes:  g.Scopes,
		Origin:  g.Origin,
	}, h.grantTTL, g.Deadline, now)
	if err != nil {
		return RefreshedGrant{}, err
	}
	expires := time.Unix(now.Add(h.grantTTL).Unix(), 0)
	if expires.After(g.Deadline) {
		expires = g.Deadline
	}
	return RefreshedGrant{Token: next, Expires: expires}, nil
}

// Prune drops burned nonces whose grants have expired. Wire it to a cron if the
// app runs long enough for the table to matter; nothing depends on it for
// correctness.
func (h *Host) Prune(ctx context.Context) error {
	return h.store.Prune(ctx, time.Now())
}
