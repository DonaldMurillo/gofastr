package embed

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/routeseg"
)

// Default token lifetimes.
const (
	DefaultNonceTTL = time.Minute
	DefaultGrantTTL = 15 * time.Minute
	// DefaultGrantMaxAge is how long a frame may refresh before it needs a new nonce.
	DefaultGrantMaxAge = 12 * time.Hour
)

// DefaultMaxThemeVariants caps the distinct customer themes one surface registers.
const DefaultMaxThemeVariants = 32

// ThemeConfig restricts how far a customer may re-theme a surface.
type ThemeConfig struct {
	// AllowTokens names the style tokens a customer may override; empty means none.
	AllowTokens []string
	// MaxVariants caps registered themes; zero uses DefaultMaxThemeVariants.
	MaxVariants int
}

// Screen is the app route a surface renders; *app.Screen satisfies it.
type Screen interface {
	// RoutePath is the route the screen is mounted at, e.g. "/reports/:id".
	RoutePath() string
}

// Surface is one embeddable piece of the app.
type Surface struct {
	// Name is the snippet's id: lowercase letters, digits and dashes, at most 64 bytes.
	Name string
	// Screen is the app screen rendered inside the frame; pass the registered *app.Screen. Required.
	Screen Screen
	// Origins lists the exact origins allowed to frame this surface. Required.
	Origins []string
	// Scopes is the capability set a nonce may carry; MintNonce may only narrow it.
	Scopes []string
	// Theme restricts customer re-theming. Zero value means not re-themable.
	Theme ThemeConfig
	// Reach lists extra path prefixes a grant may reach beyond the surface's route and the runtime.
	Reach []string
}

// SubjectResolver maps a grant subject to the request's user; an error fails closed.
type SubjectResolver func(ctx context.Context, subject string) (any, error)

// TenantResolver maps a grant subject to its tenant id; an error fails closed.
type TenantResolver func(ctx context.Context, subject string) (string, error)

// Config declares an app's embeddable surfaces.
type Config struct {
	// Surfaces is the closed set of embeddable surfaces.
	Surfaces []Surface
	// BurnStore records spent nonces. Required; use NewSQLBurnStore across replicas.
	BurnStore BurnStore
	// Resolve maps a grant subject to the current user. Optional; nil renders anonymously.
	Resolve SubjectResolver
	// ResolveTenant maps a grant subject to its tenant id. Optional.
	ResolveTenant TenantResolver
	// NonceTTL, GrantTTL and GrantMaxAge override the defaults; zero keeps them.
	NonceTTL    time.Duration
	GrantTTL    time.Duration
	GrantMaxAge time.Duration
	// OriginSource optionally supplies allowed origins per customer at request time.
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

	prevNonceKeys [][]byte
	prevGrantKeys [][]byte

	reserved []string

	widgetOnSurface func(name, surfacePath string) bool

	now func() time.Time
}

// ResolvedSurface is a declared Surface with its origins and route normalized at boot.
type ResolvedSurface struct {
	Surface
	path    string
	dynamic bool
	origins *originSet
	scopes  map[string]struct{}
}

// Path returns the normalized app route whose subtree a grant for this surface reaches.
func (s *ResolvedSurface) Path() string { return s.path }

// Dynamic reports whether the surface's route captures a parameter, making Path a pattern.
func (s *ResolvedSurface) Dynamic() bool { return s.dynamic }

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
func New(cfg Config) (*Host, error) {
	if len(cfg.Surfaces) == 0 {
		return nil, errors.New("embed: at least one Surface is required")
	}
	if cfg.BurnStore == nil {
		return nil, errors.New("embed: a BurnStore is required: single-use nonces cannot be enforced without one")
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
		now:           time.Now,
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
		return nil, fmt.Errorf("embed: GrantMaxAge (%s) must be greater than GrantTTL (%s)", h.grantMaxAge, h.grantTTL)
	}
	for _, s := range cfg.Surfaces {
		if !validSurfaceName(s.Name) {
			return nil, fmt.Errorf("embed: surface name %q must be lowercase letters, digits and dashes", s.Name)
		}
		if _, dup := h.surfaces[s.Name]; dup {
			return nil, fmt.Errorf("embed: surface %q declared twice", s.Name)
		}
		if s.Screen == nil {
			return nil, fmt.Errorf("embed: surface %q: Screen is required: a surface renders a screen, not a path string", s.Name)
		}
		route := s.Screen.RoutePath()
		if !strings.HasPrefix(route, "/") {
			return nil, fmt.Errorf("embed: surface %q screen route must be an absolute app route, got %q", s.Name, route)
		}
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
		h.surfaces[s.Name] = &ResolvedSurface{Surface: s, path: norm, dynamic: isDynamicRoute(norm), origins: origins, scopes: scopes}
		h.names = append(h.names, s.Name)
	}
	sort.Strings(h.names)
	return h, nil
}

// SetKeys installs the HMAC keys for nonces and grants.
func (h *Host) SetKeys(nonceKey, grantKey []byte) {
	h.nonceKey = nonceKey
	h.grantKey = grantKey
}

// SetWidgetReach installs the check that a widget is offered on a surface's page; nil refuses every widget route.
func (h *Host) SetWidgetReach(onSurface func(name, surfacePath string) bool) {
	h.widgetOnSurface = onSurface
}

// SetPreviousKeys installs retired keys accepted for verification only.
func (h *Host) SetPreviousKeys(nonceKeys, grantKeys [][]byte) {
	h.prevNonceKeys = nonceKeys
	h.prevGrantKeys = grantKeys
}

func (h *Host) verifyNonce(token string, now time.Time) (Nonce, error) {
	return verifyWithRotation(h.nonceKey, h.prevNonceKeys, token, now, VerifyNonce)
}

func (h *Host) verifyGrant(token string, now time.Time) (Grant, error) {
	return verifyWithRotation(h.grantKey, h.prevGrantKeys, token, now, VerifyGrant)
}

func verifyWithRotation[T any](current []byte, prev [][]byte, token string, now time.Time, verify func([]byte, string, time.Time) (T, error)) (T, error) {
	v, err := verify(current, token, now)
	if err == nil {
		return v, nil
	}
	for _, k := range prev {
		if len(k) == 0 {
			continue
		}
		if v2, err2 := verify(k, token, now); err2 == nil {
			return v2, nil
		}
	}
	var zero T
	return zero, err
}

func (h *Host) replayExpired(ctx context.Context, n Nonce, now time.Time) (ExchangeResult, error) {
	issued, _, err := h.store.Burn(ctx, n.ID, "", now)
	if err != nil {
		return ExchangeResult{}, err
	}
	g, err := h.verifyGrant(issued, now)
	if err != nil {
		return ExchangeResult{}, ErrExpired
	}
	return ExchangeResult{Grant: issued, Expires: g.Expires, Replay: true, Surface: n.Surface}, nil
}

// Ready reports whether the host has signing keys.
func (h *Host) Ready() bool { return len(h.nonceKey) > 0 && len(h.grantKey) > 0 }

// Names returns the declared surface names, sorted. Never served.
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

// AllowedOrigins returns the surface's normalized allowlist, in declaration order.
func (s *ResolvedSurface) AllowedOrigins() []string { return s.origins.List() }

// AllowsOrigin reports whether candidate may frame this surface.
func (s *ResolvedSurface) AllowsOrigin(candidate string) bool { return s.origins.Has(candidate) }

// GrantTTL exposes the configured grant lifetime.
func (h *Host) GrantTTL() time.Duration { return h.grantTTL }

// TenantResolver returns the configured tenant resolver, or nil.
func (h *Host) TenantResolver() TenantResolver { return h.resolveTenant }

// Resolver returns the configured subject resolver, or nil.
func (h *Host) Resolver() SubjectResolver { return h.resolve }

// OriginSource returns the configured per-customer origin source, or nil.
func (h *Host) OriginSource() OriginSource { return h.originSource }

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

// MintNonce signs a single-use nonce for one viewer of one surface on one origin;
// scopes narrows the surface's scopes (nil grants all, an undeclared one errors).
func (h *Host) MintNonce(ctx context.Context, surfaceName, subject, origin string, scopes []string) (string, error) {
	if !h.Ready() {
		return "", errors.New("embed: no signing key: set an app secret (WithSecret or GOFASTR_SECRET)")
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
	// Replay is true when the exchange returned a previously issued grant.
	Replay bool
	// Surface names the surface the nonce was minted for.
	Surface string
}

// ErrSpent means the nonce was already exchanged and its grant window closed.
var ErrSpent = errors.New("embed: nonce already used")

// Exchange verifies a nonce, burns it, and returns its grant; a repeat returns
// the stored grant. framedOrigin must match the nonce's origin.
func (h *Host) Exchange(ctx context.Context, nonceToken, framedOrigin string) (ExchangeResult, error) {
	if !h.Ready() {
		return ExchangeResult{}, errors.New("embed: no signing key")
	}
	now := h.now()
	n, err := h.verifyNonce(nonceToken, now)
	expired := false
	if err != nil {
		aged, agedErr := h.verifyNonce(nonceToken, time.Time{})
		if agedErr != nil {
			return ExchangeResult{}, err
		}
		n, expired = aged, true
	}
	s, ok := h.surfaces[n.Surface]
	if !ok {
		return ExchangeResult{}, fmt.Errorf("embed: unknown surface %q", n.Surface)
	}
	allowedOrigin, err := h.originAllowed(ctx, s, n.Surface, n.Origin)
	if err != nil {
		return ExchangeResult{}, err
	}
	if !allowedOrigin {
		return ExchangeResult{}, fmt.Errorf("embed: origin %q is no longer allowed to frame surface %q", n.Origin, n.Surface)
	}
	reported, err := NormalizeOrigin(framedOrigin)
	if err != nil || reported != n.Origin {
		return ExchangeResult{}, fmt.Errorf("embed: nonce was minted for %q but the frame reports %q", n.Origin, framedOrigin)
	}
	if expired {
		return h.replayExpired(ctx, n, now)
	}

	deadline := time.Unix(now.Add(h.grantMaxAge).Unix(), 0)
	grant, expires, err := mintGrant(h.grantKey, n, h.grantTTL, deadline, now)
	if err != nil {
		return ExchangeResult{}, err
	}
	retain := expires
	if n.Expires.After(retain) {
		retain = n.Expires
	}
	issued, replay, err := h.store.Burn(ctx, n.ID, grant, retain)
	if err != nil {
		return ExchangeResult{}, err
	}
	if issued == "" {
		if !h.now().Before(retain) {
			return ExchangeResult{}, ErrExpired
		}
		return ExchangeResult{}, ErrSpent
	}
	if replay {
		g, err := h.verifyGrant(issued, now)
		if err != nil {
			return ExchangeResult{}, ErrSpent
		}
		return ExchangeResult{Grant: issued, Expires: g.Expires, Replay: true, Surface: n.Surface}, nil
	}
	return ExchangeResult{Grant: grant, Expires: expires, Replay: false, Surface: n.Surface}, nil
}

// VerifyGrant checks a grant against current config, dropping scopes no longer declared.
func (h *Host) VerifyGrant(ctx context.Context, token string) (Grant, error) {
	g, _, err := h.verifyConfiguredGrant(ctx, token)
	return g, err
}

func (h *Host) verifyConfiguredGrant(ctx context.Context, token string) (Grant, *ResolvedSurface, error) {
	if !h.Ready() {
		return Grant{}, nil, errors.New("embed: no signing key")
	}
	g, err := h.verifyGrant(token, time.Now())
	if err != nil {
		return Grant{}, nil, err
	}
	s, ok := h.surfaces[g.Surface]
	if !ok {
		return Grant{}, nil, fmt.Errorf("embed: unknown surface %q", g.Surface)
	}
	allowedOrigin, err := h.originAllowed(ctx, s, g.Surface, g.Origin)
	if err != nil {
		return Grant{}, nil, err
	}
	if !allowedOrigin {
		return Grant{}, nil, fmt.Errorf("embed: origin %q is no longer allowed to frame surface %q", g.Origin, g.Surface)
	}
	g.Scopes = intersectScopes(g.Scopes, s.scopes)
	return g, s, nil
}

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

// ErrGrantExhausted means the grant reached its deadline; the page must load a new nonce.
var ErrGrantExhausted = errors.New("embed: grant reached its absolute deadline")

// Refresh rolls a live grant's expiry forward, never its deadline.
func (h *Host) Refresh(ctx context.Context, token string) (RefreshedGrant, error) {
	g, err := h.VerifyGrant(ctx, token)
	if err != nil {
		return RefreshedGrant{}, err
	}
	now := time.Now()
	if !now.Before(g.Deadline) {
		return RefreshedGrant{}, ErrGrantExhausted
	}
	next, expires, err := mintGrant(h.grantKey, Nonce{
		Surface: g.Surface,
		Subject: g.Subject,
		Scopes:  g.Scopes,
		Origin:  g.Origin,
	}, h.grantTTL, g.Deadline, now)
	if err != nil {
		return RefreshedGrant{}, err
	}
	return RefreshedGrant{Token: next, Expires: expires}, nil
}

// Prune drops burned nonces past their retention deadline.
func (h *Host) Prune(ctx context.Context) error {
	return h.store.Prune(ctx, time.Now())
}

func isDynamicRoute(route string) bool {
	for _, seg := range strings.Split(route, "/") {
		if param, _ := routeSeg(seg); param {
			return true
		}
	}
	return false
}

func routeSeg(seg string) (param, rest bool) {
	if strings.HasPrefix(seg, ":") {
		return true, strings.HasSuffix(seg, "*")
	}
	if len(seg) >= 3 && seg[0] == '{' && seg[len(seg)-1] == '}' {
		return true, strings.HasSuffix(seg, "...}")
	}
	return false, false
}

func segAdmits(seg, v string) bool {
	if seg[0] == '{' {
		seg = ":" + seg[1:len(seg)-1]
	}
	return v != "" && routeseg.ConstraintOK(seg, v)
}
