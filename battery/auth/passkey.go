package auth

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// PasskeyConfig configures the PasskeyPlugin: sign-in with WebAuthn
// passkeys (platform authenticators, phones, security keys).
type PasskeyConfig struct {
	// RPID is the relying party ID passkeys are scoped to: the site's
	// registrable domain or a parent of the serving host
	// ("example.com"). Required. Changing it later orphans every passkey
	// registered under the old one.
	RPID string

	// RPName is the name the browser shows in its passkey prompt.
	// Default: RPID.
	RPName string

	// Origins are the exact origins ceremonies may come from
	// ("https://app.example.com"). Required. Each must be https (http is
	// accepted for localhost only) and its host the RPID or a subdomain.
	Origins []string

	// Store keeps registered passkeys. Required outside DevMode unless
	// AuthConfig.AllowInMemoryStores: NewSQLPasskeyStore(db).
	Store PasskeyStore

	// ChallengeStore keeps issued challenges until they are spent once.
	// It is the token store the password-reset and magic-link flows use
	// (challenges are tagged with their own purpose), so one
	// NewSQLMagicLinkTokenStore(db) can serve all of them. Required
	// outside DevMode unless AuthConfig.AllowInMemoryStores.
	ChallengeStore MagicLinkTokenStore

	// ChallengeTTL bounds how long a ceremony may take. Default: 5 min.
	ChallengeTTL time.Duration

	// OnSuccessURL is where the browser goes after a passkey sign-in.
	// Same-origin paths only. Default: "/".
	OnSuccessURL string

	// TwoFactorURL is where a sign-in goes when the account still owes
	// its second factor: the app's page that posts the code to
	// /2fa/challenge. Same-origin paths only. Required when a 2FA plugin
	// is registered.
	TwoFactorURL string

	// MaxPerUser caps how many passkeys one account may register.
	// Default: 20.
	MaxPerUser int

	// RateLimit applies a per-IP limit to every passkey endpoint.
	// Default: 30 attempts/min with a 5-minute block.
	RateLimit *RateLimiterConfig

	// ReauthTTL is how long the proof a passed step-up returns stays
	// good for RedeemReauthProof. Default: 5 min.
	ReauthTTL time.Duration

	// NoticeSender emails the account's verified address when a passkey
	// is added to it, so a passkey someone else added does not go
	// unnoticed. The body is fixed text with the time and the kind of
	// passkey, never a caller-chosen value such as its name. Init refuses
	// a plugin without one outside DevMode unless NoAddedNotice is set.
	NoticeSender EmailSender

	// NoAddedNotice turns the added-passkey email off, for a host that
	// tells its users some other way.
	NoAddedNotice bool

	// ReauthMinAge is how old a passkey must be to confirm a step-up;
	// it must also predate the session. A stolen session can register a
	// passkey, so a new one proves nothing. Default: 1 hour; negative
	// drops the age rule and keeps only the predate rule.
	ReauthMinAge time.Duration
}

// PasskeyCredential is one registered passkey.
type PasskeyCredential struct {
	ID             []byte
	UserID         string
	PublicKey      []byte // COSE_Key, as the authenticator sent it
	Algorithm      int64  // COSE algorithm: -7 ES256, -8 EdDSA, -257 RS256
	SignCount      uint32
	Name           string
	BackupEligible bool
	BackedUp       bool
	// Transports are the ways the authenticator said it can be reached
	// ("internal", "hybrid", "usb", "nfc", "ble", "smart-card"), sent
	// back in excludeCredentials and allowCredentials.
	Transports []string
	// AAGUID names the authenticator's make and model (16 bytes); all
	// zero when the authenticator withholds it.
	AAGUID     []byte
	CreatedAt  time.Time
	LastUsedAt time.Time
}

// DeviceType is "multiDevice" for a passkey that can sync between
// devices (backup eligible) and "singleDevice" for one bound to its
// authenticator.
func (c PasskeyCredential) DeviceType() string {
	if c.BackupEligible {
		return "multiDevice"
	}
	return "singleDevice"
}

var (
	// ErrPasskeyNotFound is returned when no passkey has the id.
	ErrPasskeyNotFound = errors.New("auth: passkey not found")
	// ErrPasskeyExists is returned when a passkey id is already registered.
	ErrPasskeyExists = errors.New("auth: passkey already registered")
	// ErrPasskeyLimit is returned when the user already holds the most
	// passkeys AddPasskey was told to allow.
	ErrPasskeyLimit = errors.New("auth: passkey limit reached")
)

// PasskeyStore persists passkeys. Implementations must make AddPasskey
// refuse a duplicate id and RecordPasskeyUse a compare-and-swap.
type PasskeyStore interface {
	// AddPasskey stores c, refusing a duplicate id with ErrPasskeyExists
	// and, when the user already holds maxPerUser passkeys or more,
	// ErrPasskeyLimit. The count and the insert are one atomic step: two
	// registrations racing for the last slot store one passkey, not two.
	AddPasskey(ctx context.Context, c PasskeyCredential, maxPerUser int) error
	GetPasskey(ctx context.Context, id []byte) (PasskeyCredential, error)
	ListPasskeys(ctx context.Context, userID string) ([]PasskeyCredential, error)
	// RecordPasskeyUse moves the sign count from old to next and stamps
	// the use, only while the stored count is still old. It reports
	// whether it wrote: false means another sign-in got there first.
	RecordPasskeyUse(ctx context.Context, id []byte, old, next uint32, backedUp bool, at time.Time) (bool, error)
	// RenamePasskey sets the name of the user's passkey; false when the
	// user has no passkey with that id.
	RenamePasskey(ctx context.Context, userID string, id []byte, name string) (bool, error)
	// DeletePasskey removes the user's passkey; false when the user has
	// no passkey with that id.
	DeletePasskey(ctx context.Context, userID string, id []byte) (bool, error)
	// DeletePasskeysForUser removes every passkey the user has.
	DeletePasskeysForUser(ctx context.Context, userID string) (int, error)
}

// MemoryPasskeyStore is a goroutine-safe in-memory PasskeyStore for
// development and tests. A restart forgets every passkey.
type MemoryPasskeyStore struct {
	mu    sync.Mutex
	creds map[string]PasskeyCredential
}

// NewMemoryPasskeyStore returns an empty in-memory store.
func NewMemoryPasskeyStore() *MemoryPasskeyStore {
	return &MemoryPasskeyStore{creds: map[string]PasskeyCredential{}}
}

func clonePasskey(c PasskeyCredential) PasskeyCredential {
	c.ID = slices.Clone(c.ID)
	c.PublicKey = slices.Clone(c.PublicKey)
	c.Transports = slices.Clone(c.Transports)
	c.AAGUID = slices.Clone(c.AAGUID)
	return c
}

// AddPasskey implements PasskeyStore.
func (m *MemoryPasskeyStore) AddPasskey(_ context.Context, c PasskeyCredential, maxPerUser int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.creds[string(c.ID)]; ok {
		return ErrPasskeyExists
	}
	held := 0
	for _, have := range m.creds {
		if have.UserID == c.UserID {
			held++
		}
	}
	if held >= maxPerUser {
		return ErrPasskeyLimit
	}
	m.creds[string(c.ID)] = clonePasskey(c)
	return nil
}

// GetPasskey implements PasskeyStore.
func (m *MemoryPasskeyStore) GetPasskey(_ context.Context, id []byte) (PasskeyCredential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.creds[string(id)]
	if !ok {
		return PasskeyCredential{}, ErrPasskeyNotFound
	}
	return clonePasskey(c), nil
}

// ListPasskeys implements PasskeyStore, oldest first.
func (m *MemoryPasskeyStore) ListPasskeys(_ context.Context, userID string) ([]PasskeyCredential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []PasskeyCredential
	for _, k := range slices.Sorted(maps.Keys(m.creds)) {
		if c := m.creds[k]; c.UserID == userID {
			out = append(out, clonePasskey(c))
		}
	}
	slices.SortStableFunc(out, func(a, b PasskeyCredential) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out, nil
}

// RecordPasskeyUse implements PasskeyStore.
func (m *MemoryPasskeyStore) RecordPasskeyUse(_ context.Context, id []byte, old, next uint32, backedUp bool, at time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.creds[string(id)]
	if !ok || c.SignCount != old {
		return false, nil
	}
	c.SignCount, c.BackedUp, c.LastUsedAt = next, backedUp, at
	m.creds[string(id)] = c
	return true, nil
}

// RenamePasskey implements PasskeyStore.
func (m *MemoryPasskeyStore) RenamePasskey(_ context.Context, userID string, id []byte, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.creds[string(id)]
	if !ok || c.UserID != userID {
		return false, nil
	}
	c.Name = name
	m.creds[string(id)] = c
	return true, nil
}

// DeletePasskey implements PasskeyStore.
func (m *MemoryPasskeyStore) DeletePasskey(_ context.Context, userID string, id []byte) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.creds[string(id)]
	if !ok || c.UserID != userID {
		return false, nil
	}
	delete(m.creds, string(id))
	return true, nil
}

// DeletePasskeysForUser implements PasskeyStore.
func (m *MemoryPasskeyStore) DeletePasskeysForUser(_ context.Context, userID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, k := range slices.Sorted(maps.Keys(m.creds)) {
		if m.creds[k].UserID == userID {
			delete(m.creds, k)
			n++
		}
	}
	return n, nil
}

// PasskeyPlugin adds passkey sign-in and per-account passkey management:
//
//	POST {base}/passkey/login/options     anonymous; returns request options
//	POST {base}/passkey/login             anonymous; verifies, mints a session
//	POST {base}/passkey/register/options  signed in; returns creation options
//	POST {base}/passkey/register          signed in; stores the passkey
//	GET  {base}/passkey/list              signed in; the account's passkeys
//	POST {base}/passkey/rename            signed in; renames one
//	POST {base}/passkey/delete            signed in; removes one
//	POST {base}/passkey/reauth/options    signed in; a step-up challenge
//	POST {base}/passkey/reauth            signed in; verifies, returns a proof
//
// Every endpoint speaks JSON (a cross-site page cannot send that without a
// CORS preflight the battery never answers). The browser half is the
// "passkey" and "passkey-manage" runtime behaviours (passkey.js,
// passkey_manage.js), bound to data-passkey-* elements; buttons stay
// hidden on a browser without WebAuthn.
type PasskeyPlugin struct {
	config     PasskeyConfig
	rp         webauthnRP
	mgr        *AuthManager
	store      PasskeyStore
	challenges MagicLinkTokenStore
	notices    *mailQueue
	limit      *RateLimiter
	// listLimit guards GET /passkey/list and autofillLimit the autofill
	// field's login/options requests: the browser module sends both
	// without a click, so they must not spend the ceremonies' budget.
	listLimit     *RateLimiter
	autofillLimit *RateLimiter
}

// NewPasskeyPlugin builds the plugin. RPID and Origins are checked at Init.
func NewPasskeyPlugin(cfg PasskeyConfig) *PasskeyPlugin {
	if cfg.RPName == "" {
		cfg.RPName = cfg.RPID
	}
	if cfg.ChallengeTTL <= 0 {
		cfg.ChallengeTTL = 5 * time.Minute
	}
	if cfg.OnSuccessURL == "" {
		cfg.OnSuccessURL = "/"
	}
	if cfg.MaxPerUser <= 0 {
		cfg.MaxPerUser = 20
	}
	if cfg.ReauthTTL <= 0 {
		cfg.ReauthTTL = 5 * time.Minute
	}
	switch {
	case cfg.ReauthMinAge == 0:
		cfg.ReauthMinAge = time.Hour
	case cfg.ReauthMinAge < 0:
		cfg.ReauthMinAge = 0
	}
	if cfg.RateLimit == nil {
		cfg.RateLimit = &RateLimiterConfig{MaxAttempts: 30, Window: time.Minute, BlockDuration: 5 * time.Minute}
	}
	return &PasskeyPlugin{
		config:        cfg,
		rp:            webauthnRP{ID: cfg.RPID, Origins: slices.Clone(cfg.Origins)},
		notices:       newMailQueue("passkey"),
		limit:         newScopedRateLimiter(*cfg.RateLimit, "passkey"),
		listLimit:     newScopedRateLimiter(subScope(*cfg.RateLimit, "-list"), "passkey-list"),
		autofillLimit: newScopedRateLimiter(subScope(*cfg.RateLimit, "-autofill"), "passkey-autofill"),
	}
}

// subScope gives a background limiter its own key under a host's
// explicit RateLimit.Scope, so its requests never spend the ceremonies'
// budget in a shared Store; an empty Scope keeps the default.
func subScope(cfg RateLimiterConfig, suffix string) RateLimiterConfig {
	if cfg.Scope != "" {
		cfg.Scope += suffix
	}
	return cfg
}

// Name returns the plugin identifier.
func (p *PasskeyPlugin) Name() string { return "passkey" }

// Init checks the RP configuration and picks the stores. Like the 2FA
// plugin it refuses in-memory stores in production: a restart would
// forget every passkey, and a second replica would not know the
// challenges the first one issued.
func (p *PasskeyPlugin) Init(mgr *AuthManager) error {
	if err := p.rp.validate(); err != nil {
		return fmt.Errorf("auth: passkey plugin: %w", err)
	}
	p.mgr = mgr
	cfg := mgr.Config()
	memoryOK := cfg.DevMode || cfg.AllowInMemoryStores
	p.store, p.challenges = p.config.Store, p.config.ChallengeStore
	if p.store == nil || p.challenges == nil {
		if !memoryOK {
			return errors.New("auth: passkey plugin: production mode refuses in-memory stores: a restart forgets every passkey and replicas cannot share challenges; set PasskeyConfig.Store (auth.NewSQLPasskeyStore(db, \"auth_passkeys\")) and PasskeyConfig.ChallengeStore (auth.NewSQLMagicLinkTokenStore(db)), or AuthConfig.AllowInMemoryStores: true for a deliberate single-node deployment")
		}
		if p.store == nil {
			p.store = NewMemoryPasskeyStore()
		}
		if p.challenges == nil {
			p.challenges = NewMemoryMagicLinkTokenStore()
		}
	}
	if p.config.NoticeSender == nil && !p.config.NoAddedNotice && !cfg.DevMode {
		return errors.New("auth: passkey plugin: set PasskeyConfig.NoticeSender so the account owner is emailed when a passkey is added, or PasskeyConfig.NoAddedNotice: true if the app tells them another way")
	}
	if p.config.NoticeSender != nil && !p.config.NoAddedNotice {
		if _, ok := mgr.UserStore().(EmailVerifiedChecker); !ok {
			return errors.New("auth: passkey plugin: the added-passkey notice goes only to a verified address, and the user store cannot say which are (it does not implement auth.EmailVerifiedChecker); implement it, or set PasskeyConfig.NoAddedNotice: true and tell the owner another way")
		}
	}
	if err := checkTwoFactorURL("passkey", "PasskeyConfig.TwoFactorURL", p.config.TwoFactorURL); err != nil {
		return err
	}
	// Every plugin is registered before any Init runs, so a 2FA plugin
	// later in the list is already visible here.
	if p.config.TwoFactorURL == "" {
		for _, name := range mgr.order {
			if _, ok := mgr.plugins[name].(TwoFactorChecker); ok {
				return fmt.Errorf("auth: passkey plugin: %q handles two-factor sign-in, so set PasskeyConfig.TwoFactorURL to the page that finishes it: a passkey sign-in of a 2FA account is pending until /2fa/challenge, and OnSuccessURL would refuse it", name)
			}
		}
	}
	if se, ok := p.store.(interface{ EnsureSchema(context.Context) error }); ok {
		if err := se.EnsureSchema(context.Background()); err != nil {
			return fmt.Errorf("auth: passkey store EnsureSchema: %w", err)
		}
	}
	return nil
}

// RegisterRoutes mounts the passkey endpoints.
func (p *PasskeyPlugin) RegisterRoutes(r *router.Router, basePath string) {
	r = r.Group("", browserAccountOnly)
	r.Post(basePath+"/passkey/login/options", http.HandlerFunc(p.loginOptionsHandler))
	r.Post(basePath+"/passkey/login", http.HandlerFunc(p.loginHandler))
	r.Post(basePath+"/passkey/register/options", http.HandlerFunc(p.registerOptionsHandler))
	r.Post(basePath+"/passkey/register", http.HandlerFunc(p.registerHandler))
	r.Get(basePath+"/passkey/list", http.HandlerFunc(p.listHandler))
	r.Post(basePath+"/passkey/rename", http.HandlerFunc(p.renameHandler))
	r.Post(basePath+"/passkey/delete", http.HandlerFunc(p.deleteHandler))
	r.Post(basePath+"/passkey/reauth/options", http.HandlerFunc(p.reauthOptionsHandler))
	r.Post(basePath+"/passkey/reauth", http.HandlerFunc(p.reauthHandler))
}

// maxUserHandleLen is WebAuthn's ceiling for user.id.
const maxUserHandleLen = 64

var b64url = base64.RawURLEncoding

// requireFullSession resolves a session that may manage the account's
// passkeys: signed in, not waiting on a second factor, and past the
// second factor when the account has one. Adding a passkey adds a way
// in, so a password-only session of a 2FA account must not do it.
func (p *PasskeyPlugin) requireFullSession(w http.ResponseWriter, r *http.Request) (*Session, bool) {
	if rejectInProcessAccountOperation(w, r) {
		return nil, false
	}
	sess, status, reason := p.fullSession(r)
	if sess == nil {
		writeAuthError(w, r, status, reason)
		return nil, false
	}
	return sess, true
}

// fullSession is requireFullSession's judgement without the answer: the
// session, or the status and reason refusing it.
func (p *PasskeyPlugin) fullSession(r *http.Request) (*Session, int, i18n.Phrase) {
	sess, err := p.mgr.requestSession(r, false)
	if err != nil {
		return nil, http.StatusUnauthorized, reasonNotAuthenticated
	}
	if sess.PendingTwoFactor {
		return nil, http.StatusForbidden, reason2faRequired
	}
	if !sess.TwoFactorVerified {
		on, err := p.mgr.twoFactorEnabled(r.Context(), sess.UserID)
		if err != nil {
			return nil, http.StatusInternalServerError, reason2faLookupFailed
		}
		if on {
			return nil, http.StatusForbidden, reason2faRequired
		}
	}
	return sess, 0, i18n.Phrase{}
}

// decodeB64Fields decodes base64url members of a request body, refusing
// padding and empty values.
func decodeB64Fields(fields map[string]string) (map[string][]byte, error) {
	out := make(map[string][]byte, len(fields))
	for _, k := range slices.Sorted(maps.Keys(fields)) {
		b, err := b64url.Strict().DecodeString(fields[k])
		if err != nil || len(b) == 0 {
			return nil, i18n.NewError(reasonNotBase64URL, map[string]any{"field": k})
		}
		out[k] = b
	}
	return out, nil
}

// requireJSON refuses a POST that is not application/json. The options
// endpoints read no body, but a JSON content type is what makes a
// cross-site page send a CORS preflight before reaching them.
func requireJSON(w http.ResponseWriter, r *http.Request) bool {
	if !handler.IsJSONContentType(r.Header.Get("Content-Type")) {
		writeAuthError(w, r, http.StatusUnsupportedMediaType, i18n.ReasonJSONMediaType)
		return false
	}
	return true
}

// ─── registration ─────────────────────────────────────────────────────

func (p *PasskeyPlugin) registerOptionsHandler(w http.ResponseWriter, r *http.Request) {
	if !p.limit.guard(w, r) || !requireJSON(w, r) {
		return
	}
	sess, ok := p.requireFullSession(w, r)
	if !ok {
		return
	}
	opts := readOptions(w, r)
	conditional := opts.flag("conditional")
	if len(sess.UserID) > maxUserHandleLen {
		writeAuthError(w, r, http.StatusInternalServerError, reasonUserIdTooLong)
		return
	}
	userStore := p.mgr.UserStore()
	if userStore == nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonUserStoreNotConfigured)
		return
	}
	user, err := userStore.FindByID(r.Context(), sess.UserID)
	if errors.Is(err, ErrUserNotFound) {
		writeAuthError(w, r, http.StatusUnauthorized, reasonNotAuthenticated)
		return
	}
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonUserLookupFailed)
		return
	}
	existing, err := p.store.ListPasskeys(r.Context(), sess.UserID)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonPasskeyLookupFailed)
		return
	}
	if len(existing) >= p.config.MaxPerUser {
		writeAuthError(w, r, http.StatusConflict, reasonPasskeyLimit)
		return
	}
	if !p.steppedUp(w, r, sess, existing, opts.text("proof")) {
		return
	}
	purpose, uv := purposePasskeyRegister, "required"
	if conditional {
		purpose, uv = purposePasskeyRegisterQuiet, "preferred"
	}
	challenge, err := createPurposeToken(r.Context(), p.challenges, purpose, sess.UserID, p.config.ChallengeTTL)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonChallengeCreateFailed)
		return
	}
	// The challenge's payload stays the bare user id, which erasure finds;
	// a second token ties it to this session, which earned any step-up.
	binding, err := createPurposeToken(r.Context(), p.challenges, purposePasskeyRegisterSession, registrationBinding(challenge, sess), p.config.ChallengeTTL)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonChallengeCreateFailed)
		return
	}
	params := make([]map[string]any, 0, len(passkeyAlgs))
	for _, alg := range passkeyAlgs {
		params = append(params, map[string]any{"type": "public-key", "alg": alg})
	}
	writeAuthJSON(w, r, http.StatusOK, map[string]any{"binding": binding, "publicKey": map[string]any{
		"rp":                 map[string]string{"id": p.rp.ID, "name": p.config.RPName},
		"user":               map[string]string{"id": b64url.EncodeToString([]byte(sess.UserID)), "name": user.GetEmail(), "displayName": user.GetEmail()},
		"challenge":          b64url.EncodeToString([]byte(challenge)),
		"pubKeyCredParams":   params,
		"timeout":            p.config.ChallengeTTL.Milliseconds(),
		"attestation":        "none",
		"excludeCredentials": credentialDescriptors(existing),
		"authenticatorSelection": map[string]any{
			"residentKey": "required", "requireResidentKey": true, "userVerification": uv,
		},
	}})
}

// registrationBinding is the payload that ties a registration challenge
// to the session that asked for it.
func registrationBinding(challenge string, sess *Session) string {
	return sha256hex(challenge) + "|" + proofPayload(sess)
}

// optionsBody is an options route's optional JSON body. The options
// routes read no body before these members, so any other body, unknown
// members and unparsable JSON included, reads as empty.
type optionsBody map[string]json.RawMessage

func readOptions(w http.ResponseWriter, r *http.Request) optionsBody {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAuthBodyBytes))
	if err != nil {
		return nil
	}
	var body optionsBody
	// Strict: a duplicated or case-folded key reads as no body at all.
	if handler.UnmarshalStrict(raw, &body) != nil {
		return nil
	}
	return body
}

// flag reports whether the body holds {name: true}.
func (b optionsBody) flag(name string) bool {
	var on bool
	return json.Unmarshal(b[name], &on) == nil && on
}

// text is the body's string member name, or "".
func (b optionsBody) text(name string) string {
	var s string
	if json.Unmarshal(b[name], &s) != nil {
		return ""
	}
	return s
}

// credentialDescriptors lists creds for excludeCredentials or
// allowCredentials, with the transports each authenticator reported.
func credentialDescriptors(creds []PasskeyCredential) []map[string]any {
	out := make([]map[string]any, 0, len(creds))
	for _, c := range creds {
		d := map[string]any{"type": "public-key", "id": b64url.EncodeToString(c.ID)}
		if len(c.Transports) > 0 {
			d["transports"] = c.Transports
		}
		out = append(out, d)
	}
	return out
}

// knownTransports are the AuthenticatorTransport values the battery
// keeps; the browser may report others, which are dropped.
var knownTransports = []string{"ble", "hybrid", "internal", "nfc", "smart-card", "usb"}

// passkeyTransports keeps the known transports in ts, sorted and once
// each.
func passkeyTransports(ts []string) []string {
	var out []string
	for _, t := range ts {
		if slices.Contains(knownTransports, t) && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	slices.Sort(out)
	return out
}

// passkeyName cleans the label a user gives a passkey: trimmed, control
// and format characters refused, at most 64 characters.
func passkeyName(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Passkey", true
	}
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > 64 {
		return "", false
	}
	for _, c := range s {
		if unicode.IsControl(c) || unicode.Is(unicode.Cf, c) {
			return "", false
		}
	}
	return s, true
}

func (p *PasskeyPlugin) registerHandler(w http.ResponseWriter, r *http.Request) {
	if !p.limit.guard(w, r) {
		return
	}
	sess, ok := p.requireFullSession(w, r)
	if !ok {
		return
	}
	// Taken before the body is read, as token creation does: a
	// registration held open across a reset stored its key after the
	// reset's purge and kept a second lasting credential.
	snap, err := p.mgr.snapshotCredential(r.Context(), sess.UserID)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonUserLookupFailed)
		return
	}
	var body struct {
		Name              string   `json:"name"`
		ID                string   `json:"id"`
		ClientDataJSON    string   `json:"clientDataJSON"`
		AttestationObject string   `json:"attestationObject"`
		Transports        []string `json:"transports"`
		Binding           string   `json:"binding"`
	}
	if !decodeJSONLimited(w, r, &body) {
		return
	}
	name, ok := passkeyName(body.Name)
	if !ok {
		writeAuthError(w, r, http.StatusBadRequest, reasonNameTooLong)
		return
	}
	f, err := decodeB64Fields(map[string]string{"id": body.ID, "clientDataJSON": body.ClientDataJSON, "attestationObject": body.AttestationObject})
	if err != nil {
		writeAuthErrorFor(w, r, http.StatusBadRequest, err, i18n.StatusPhrase(http.StatusBadRequest))
		return
	}
	challenge, err := p.rp.checkClientData(f["clientDataJSON"], "webauthn.create")
	if err != nil {
		p.refuse(w, r, sess.UserID, "register", "client_data", err)
		return
	}
	// Spent before the attestation is read: a challenge is good for one
	// attempt, and it must be one this account asked for.
	purpose, owner, err := redeemAnyPurpose(r.Context(), p.challenges, challenge, purposePasskeyRegister, purposePasskeyRegisterQuiet)
	if err != nil || owner != sess.UserID {
		p.refuse(w, r, sess.UserID, "register", "challenge", errors.New("challenge unknown, spent, expired or another account's"))
		return
	}
	// Only the session that asked for the challenge, and so passed its
	// step-up, completes it: another session of the account cannot.
	if bound, err := redeemPurposeToken(r.Context(), p.challenges, purposePasskeyRegisterSession, body.Binding); body.Binding == "" || err != nil || bound != registrationBinding(challenge, sess) {
		p.refuse(w, r, sess.UserID, "register", "session", errors.New("challenge was issued to another session"))
		return
	}
	res, err := p.rp.verifyAttestation(f["attestationObject"], purpose == purposePasskeyRegisterQuiet)
	if err != nil {
		p.refuse(w, r, sess.UserID, "register", "attestation", err)
		return
	}
	if string(res.credID) != string(f["id"]) {
		p.refuse(w, r, sess.UserID, "register", "credential_mismatch", errors.New("credential id does not match the attestation"))
		return
	}
	now := time.Now().UTC()
	cred := PasskeyCredential{
		ID: res.credID, UserID: sess.UserID, PublicKey: res.publicKey, Algorithm: res.alg,
		SignCount: res.signCount, Name: name, BackupEligible: res.backupEligible, BackedUp: res.backedUp,
		Transports: passkeyTransports(body.Transports), AAGUID: res.aaguid, CreatedAt: now,
	}
	// The options call checked the cap, but challenges collected while
	// under it would each register past it, so the store counts again as
	// it inserts: a count here and then a store let two registrations
	// racing for the last slot both through.
	if err := p.store.AddPasskey(r.Context(), cred, p.config.MaxPerUser); err != nil {
		if errors.Is(err, ErrPasskeyExists) {
			writeAuthError(w, r, http.StatusConflict, reasonPasskeyAlreadyRegistered)
			return
		}
		if errors.Is(err, ErrPasskeyLimit) {
			writeAuthError(w, r, http.StatusConflict, reasonPasskeyLimit)
			return
		}
		writeAuthError(w, r, http.StatusInternalServerError, reasonPasskeySaveFailed)
		return
	}
	// The hash catches a reset or change; the session catches an account
	// claim, which may leave a passwordless hash where it was.
	if _, err := p.mgr.SessionStore().Get(r.Context(), sess.Token); err != nil || snap.moved(r.Context(), p.mgr) {
		undo, cancel := undoContext(r.Context())
		_, err := p.store.DeletePasskey(undo, sess.UserID, cred.ID)
		cancel()
		if err != nil {
			slog.Error("auth: removing a passkey registered across a credential change failed",
				"user_hash", hashedIdentifier(sess.UserID), "err", err)
			writeAuthError(w, r, http.StatusInternalServerError, reasonPasskeySaveFailed)
			return
		}
		writeAuthError(w, r, http.StatusUnauthorized, reasonInvalidSession)
		return
	}
	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind: "passkey.registered", UserID: sess.UserID, Remote: remoteHost(r),
		Meta: map[string]string{"alg": strconv.FormatInt(res.alg, 10), "backup_eligible": strconv.FormatBool(res.backupEligible), "conditional": strconv.FormatBool(purpose == purposePasskeyRegisterQuiet)},
	})
	p.noticeAdded(r, sess.UserID, cred)
	writeAuthJSON(w, r, http.StatusCreated, passkeyView(cred))
}

// OnStop cancels the added-passkey notices in flight and waits for them.
func (p *PasskeyPlugin) OnStop(ctx context.Context) error { return p.notices.stop(ctx) }

var _ AuthPluginOnStop = (*PasskeyPlugin)(nil)

// noticeAdded emails the account that a passkey was added, off the
// request: the sender is host code, so a panic in it is recovered and
// logged scrubbed, and a slow provider never holds the response.
func (p *PasskeyPlugin) noticeAdded(r *http.Request, userID string, cred PasskeyCredential) {
	sender := p.config.NoticeSender
	if sender == nil || p.config.NoAddedNotice {
		return
	}
	us := p.mgr.UserStore()
	if us == nil {
		return
	}
	u, err := us.FindByID(r.Context(), userID)
	if err != nil || u.GetEmail() == "" {
		slog.Warn("auth: no address for the added-passkey notice", "plugin", "passkey", "user_hash", hashedIdentifier(userID), "err", err)
		return
	}
	// An address nobody proved may be someone else's: mailing it would let
	// anyone send that person notices from the app's own domain.
	vc, ok := us.(EmailVerifiedChecker)
	if !ok {
		return
	}
	if verified, err := vc.IsEmailVerified(r.Context(), userID); err != nil || !verified {
		return
	}
	to, body := u.GetEmail(), passkeyAddedNoticeBody(cred)
	p.notices.enqueue(r.Context(), to, scrubbedNoticeSend("passkey",
		"added-passkey notice sender panicked", "added-passkey notice send failed",
		to, sender, body))
}

// passkeyAddedNoticeBody is the added-passkey email: fixed text and
// facts the server set, never a value the caller chose (the passkey's
// name could carry a link or markup into mail from the app's domain).
func passkeyAddedNoticeBody(cred PasskeyCredential) string {
	kind := "a passkey kept on one device"
	if cred.BackupEligible {
		kind = "a passkey that can sync between your devices"
	}
	return "A new passkey (" + kind + ") was added to your account on " +
		cred.CreatedAt.UTC().Format("2006-01-02 15:04 UTC") + ". " +
		"If you added it, you can ignore this message. If not, sign in, remove it " +
		"from your passkeys, and change your password."
}

// ─── sign-in ──────────────────────────────────────────────────────────

func (p *PasskeyPlugin) loginOptionsHandler(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	// The autofill field says so in the query, so the request is charged
	// to its own budget before any of its body is read.
	limit := p.limit
	if r.URL.Query().Get("autofill") == "1" {
		limit = p.autofillLimit
	}
	if !limit.guard(w, r) {
		return
	}
	challenge, err := createPurposeToken(r.Context(), p.challenges, purposePasskeyLogin, "", p.config.ChallengeTTL)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonChallengeCreateFailed)
		return
	}
	// No allowCredentials: the browser offers the passkeys it holds for
	// the RP ID, and the response names the account (userHandle), so the
	// sign-in needs no username and reveals no account list.
	writeAuthJSON(w, r, http.StatusOK, map[string]any{"publicKey": map[string]any{
		"challenge":        b64url.EncodeToString([]byte(challenge)),
		"rpId":             p.rp.ID,
		"timeout":          p.config.ChallengeTTL.Milliseconds(),
		"userVerification": "required",
		"allowCredentials": []any{},
	}})
}

func (p *PasskeyPlugin) loginHandler(w http.ResponseWriter, r *http.Request) {
	if !p.limit.guard(w, r) {
		return
	}
	f, challenge, ok := p.readAssertion(w, r, "login", "")
	if !ok {
		return
	}
	if _, err := redeemPurposeToken(r.Context(), p.challenges, purposePasskeyLogin, challenge); err != nil {
		p.refuse(w, r, "", "login", "challenge", errors.New("challenge unknown, spent or expired"))
		return
	}
	cred, ok := p.checkAssertion(w, r, "login", f, "", nil)
	if !ok {
		return
	}
	userStore := p.mgr.UserStore()
	if userStore == nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonUserStoreNotConfigured)
		return
	}
	user, err := userStore.FindByID(r.Context(), cred.UserID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			p.refuse(w, r, cred.UserID, "login", "account_gone", errors.New("the passkey's account no longer exists"))
			return
		}
		writeAuthError(w, r, http.StatusInternalServerError, reasonUserLookupFailed)
		return
	}
	sess, pending, err := p.mgr.MintSession(r.Context(), user.GetID(), p.mgr.Config().SessionTTL)
	if errors.Is(err, ErrTwoFactorEnforcement) {
		// Fail closed like a password login: no session, and the audit
		// trail says why.
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind: "login.failed", UserID: user.GetID(), Email: user.GetEmail(), Remote: remoteHost(r),
			Meta: map[string]string{"reason": "twofa_failclosed", "via": "passkey"},
		})
		writeAuthError(w, r, http.StatusInternalServerError, reasonTwoFactorUnavailable)
		return
	}
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonSessionCreateFailed)
		return
	}
	// A reset that finished after the use above was recorded revoked
	// sessions before this one existed and purged the passkey. Re-read
	// the key now the session is stored: the reset purges passkeys before
	// it revokes sessions, so a key still here means any revoke is still
	// to come and reaches this session, and a key gone drops it.
	if _, err := p.store.GetPasskey(r.Context(), cred.ID); err != nil {
		undo, cancel := undoContext(r.Context())
		delErr := p.mgr.SessionStore().Delete(undo, sess.Token)
		cancel()
		if delErr != nil {
			slog.Warn("auth: dropping a session minted under a removed passkey failed",
				"user_hash", hashedIdentifier(user.GetID()), "err", delErr)
		}
		if errors.Is(err, ErrPasskeyNotFound) {
			p.refuse(w, r, cred.UserID, "login", "passkey_removed", errors.New("the passkey was removed during sign-in"))
			return
		}
		writeAuthError(w, r, http.StatusInternalServerError, reasonPasskeyLookupFailed)
		return
	}
	kind := "login.succeeded"
	if pending {
		kind = "login.pending_2fa"
	}
	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind: kind, UserID: user.GetID(), Email: user.GetEmail(), Remote: remoteHost(r),
		Meta: map[string]string{"via": "passkey"},
	})
	mintSessionCookie(w, p.mgr.Config(), sess.Token, sess.ExpiresAt)
	out := map[string]any{"redirect": safeRedirectURL(p.config.OnSuccessURL)}
	if pending {
		out["redirect"] = safeRedirectURL(p.config.TwoFactorURL)
		out["two_factor_required"] = true
	}
	writeAuthJSON(w, r, http.StatusOK, out)
}

// assertionBody is the JSON a sign-in or step-up posts.
type assertionBody struct {
	ID                string `json:"id"`
	ClientDataJSON    string `json:"clientDataJSON"`
	AuthenticatorData string `json:"authenticatorData"`
	Signature         string `json:"signature"`
	UserHandle        string `json:"userHandle"`
}

// readAssertion decodes an assertion body and checks its client data,
// returning the decoded fields and the challenge it signed.
func (p *PasskeyPlugin) readAssertion(w http.ResponseWriter, r *http.Request, ceremony, userID string) (map[string][]byte, string, bool) {
	var body assertionBody
	if !decodeJSONLimited(w, r, &body) {
		return nil, "", false
	}
	fields := map[string]string{
		"id": body.ID, "clientDataJSON": body.ClientDataJSON, "authenticatorData": body.AuthenticatorData,
		"signature": body.Signature, "userHandle": body.UserHandle,
	}
	// An account-bound ceremony names its passkeys in allowCredentials,
	// and then WebAuthn lets the authenticator leave userHandle null.
	if userID != "" && body.UserHandle == "" {
		delete(fields, "userHandle")
	}
	f, err := decodeB64Fields(fields)
	if err != nil {
		writeAuthErrorFor(w, r, http.StatusBadRequest, err, i18n.StatusPhrase(http.StatusBadRequest))
		return nil, "", false
	}
	challenge, err := p.rp.checkClientData(f["clientDataJSON"], "webauthn.get")
	if err != nil {
		p.refuse(w, r, userID, ceremony, "client_data", err)
		return nil, "", false
	}
	return f, challenge, true
}

// checkAssertion verifies an assertion against the stored passkey and
// records its use: the passkey exists (and, when owner is set, belongs
// to owner), eligible (when set) accepts it, the user handle names its
// account, the signature verifies, backup eligibility is unchanged and
// the counter moved forward. eligible runs on the one read the rest of
// the checks use, so a lookup error cannot skip it.
func (p *PasskeyPlugin) checkAssertion(w http.ResponseWriter, r *http.Request, ceremony string, f map[string][]byte, owner string, eligible func(PasskeyCredential) bool) (PasskeyCredential, bool) {
	cred, err := p.store.GetPasskey(r.Context(), f["id"])
	if err == nil && owner != "" && cred.UserID != owner {
		err = ErrPasskeyNotFound
	}
	if err != nil {
		if !errors.Is(err, ErrPasskeyNotFound) {
			writeAuthError(w, r, http.StatusInternalServerError, reasonPasskeyLookupFailed)
			return PasskeyCredential{}, false
		}
		p.refuse(w, r, owner, ceremony, "unknown_credential", errors.New("unknown credential id"))
		return PasskeyCredential{}, false
	}
	if eligible != nil && !eligible(cred) {
		p.refuseTooNew(w, r, cred.UserID)
		return PasskeyCredential{}, false
	}
	// Until the signature verifies, an anonymous caller may be anyone
	// holding the credential id: its refusals name no account, so no one
	// can write refused events under another user's id.
	claimed := owner
	// The authenticator names the account it holds the passkey for; it
	// must be the account the passkey was registered to.
	// readAssertion drops an empty handle only for an account-bound
	// ceremony; any handle given must be the passkey's account.
	if handle, given := f["userHandle"]; given && string(handle) != cred.UserID {
		p.refuse(w, r, claimed, ceremony, "user_handle", errors.New("user handle does not match the passkey's account"))
		return PasskeyCredential{}, false
	}
	res, err := p.rp.verifyAssertion(cred.PublicKey, f["clientDataJSON"], f["authenticatorData"], f["signature"])
	if err != nil {
		p.refuse(w, r, claimed, ceremony, "signature", err)
		return PasskeyCredential{}, false
	}
	// Backup eligibility is fixed when the credential is made; a flip
	// means this is not the authenticator that registered it.
	if res.backupEligible != cred.BackupEligible {
		p.refuse(w, r, cred.UserID, ceremony, "backup_eligibility", errors.New("backup eligibility differs from registration"))
		return PasskeyCredential{}, false
	}
	// A counter that fails to move forward means two copies of the key
	// exist. Synced passkeys always report zero, which is no signal.
	if (res.signCount != 0 || cred.SignCount != 0) && res.signCount <= cred.SignCount {
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind: "passkey.clone_suspected", UserID: cred.UserID, Remote: remoteHost(r),
			Meta: map[string]string{"stored": strconv.FormatUint(uint64(cred.SignCount), 10), "presented": strconv.FormatUint(uint64(res.signCount), 10)},
		})
		writeAuthError(w, r, http.StatusUnauthorized, reasonPasskeySignIn)
		return PasskeyCredential{}, false
	}
	wrote, err := p.store.RecordPasskeyUse(r.Context(), cred.ID, cred.SignCount, res.signCount, res.backedUp, time.Now().UTC())
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonPasskeyUpdateFailed)
		return PasskeyCredential{}, false
	}
	if !wrote {
		writeAuthError(w, r, http.StatusConflict, reasonPasskeyConflict)
		return PasskeyCredential{}, false
	}
	return cred, true
}

// refuse answers a failed ceremony check. reason is a fixed code for
// the audit trail; why can quote the request (the anonymous login route
// takes anyone's bytes), so its text goes only to the log, scrubbed and
// bounded.
func (p *PasskeyPlugin) refuse(w http.ResponseWriter, r *http.Request, userID, ceremony, reason string, why error) {
	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind: "passkey.refused", UserID: userID, Remote: remoteHost(r),
		Meta: map[string]string{"ceremony": ceremony, "reason": reason},
	})
	slog.Info("passkey ceremony refused", "plugin", "passkey", "ceremony", ceremony,
		"user_hash", hashedIdentifier(userID), "reason", reason,
		"detail", textsafe.Truncate(textsafe.ScrubControlBytes(why.Error()), 200))
	switch {
	case ceremony != "register" && reason == "unknown_credential":
		writeAuthError(w, r, http.StatusNotFound, reasonPasskeyUnknown)
	case ceremony != "register":
		writeAuthError(w, r, http.StatusUnauthorized, reasonPasskeySignIn)
	default:
		writeAuthError(w, r, http.StatusBadRequest, reasonPasskeyRegistrationFailed)
	}
}

// ─── management ───────────────────────────────────────────────────────

func passkeyView(c PasskeyCredential) map[string]any {
	v := map[string]any{
		"id": b64url.EncodeToString(c.ID), "name": c.Name,
		"createdAt": c.CreatedAt.UTC().Format(time.RFC3339), "backedUp": c.BackedUp,
		"deviceType": c.DeviceType(), "transports": append([]string{}, c.Transports...),
	}
	if id := aaguidString(c.AAGUID); id != "" {
		v["aaguid"] = id
	}
	if !c.LastUsedAt.IsZero() {
		v["lastUsedAt"] = c.LastUsedAt.UTC().Format(time.RFC3339)
	}
	return v
}

func (p *PasskeyPlugin) listHandler(w http.ResponseWriter, r *http.Request) {
	if !p.listLimit.guard(w, r) {
		return
	}
	sess, ok := p.requireFullSession(w, r)
	if !ok {
		return
	}
	creds, err := p.store.ListPasskeys(r.Context(), sess.UserID)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonPasskeyLookupFailed)
		return
	}
	out := make([]map[string]any, 0, len(creds))
	for _, c := range creds {
		out = append(out, passkeyView(c))
	}
	// rpId, userId and the user's name let the page tell the password
	// manager which passkeys are still good (signalAllAcceptedCredentials)
	// and what the account is called (signalCurrentUserDetails).
	view := map[string]any{"rpId": p.rp.ID, "userId": b64url.EncodeToString([]byte(sess.UserID)), "passkeys": out}
	if us := p.mgr.UserStore(); us != nil {
		if u, err := us.FindByID(r.Context(), sess.UserID); err == nil {
			view["userName"] = u.GetEmail()
		}
	}
	writeAuthJSON(w, r, http.StatusOK, view)
}

// aaguidString formats an AAGUID as a UUID, or "" when it is all zero
// (the authenticator withheld it) or not 16 bytes.
func aaguidString(b []byte) string {
	if len(b) != 16 || !slices.ContainsFunc(b, func(x byte) bool { return x != 0 }) {
		return ""
	}
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func (p *PasskeyPlugin) renameHandler(w http.ResponseWriter, r *http.Request) {
	if !p.limit.guard(w, r) {
		return
	}
	sess, ok := p.requireFullSession(w, r)
	if !ok {
		return
	}
	var body struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if !decodeJSONLimited(w, r, &body) {
		return
	}
	f, err := decodeB64Fields(map[string]string{"id": body.ID})
	if err != nil {
		writeAuthErrorFor(w, r, http.StatusBadRequest, err, i18n.StatusPhrase(http.StatusBadRequest))
		return
	}
	name, ok := passkeyName(body.Name)
	if !ok {
		writeAuthError(w, r, http.StatusBadRequest, reasonNameTooLong)
		return
	}
	renamed, err := p.store.RenamePasskey(r.Context(), sess.UserID, f["id"], name)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonPasskeyUpdateFailed)
		return
	}
	if !renamed {
		writeAuthError(w, r, http.StatusNotFound, reasonPasskeyNotFound)
		return
	}
	p.mgr.emitSecurity(r.Context(), SecurityEvent{Kind: "passkey.renamed", UserID: sess.UserID, Remote: remoteHost(r)})
	w.WriteHeader(http.StatusNoContent)
}

func (p *PasskeyPlugin) deleteHandler(w http.ResponseWriter, r *http.Request) {
	if !p.limit.guard(w, r) {
		return
	}
	sess, ok := p.requireFullSession(w, r)
	if !ok {
		return
	}
	var body struct {
		ID    string `json:"id"`
		Proof string `json:"proof"`
	}
	if !decodeJSONLimited(w, r, &body) {
		return
	}
	f, err := decodeB64Fields(map[string]string{"id": body.ID})
	if err != nil {
		writeAuthErrorFor(w, r, http.StatusBadRequest, err, i18n.StatusPhrase(http.StatusBadRequest))
		return
	}
	existing, err := p.store.ListPasskeys(r.Context(), sess.UserID)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonPasskeyLookupFailed)
		return
	}
	// An id the account does not hold is answered before the step-up, so
	// a stale id does not spend the proof.
	if !slices.ContainsFunc(existing, func(c PasskeyCredential) bool { return string(c.ID) == string(f["id"]) }) {
		writeAuthError(w, r, http.StatusNotFound, reasonPasskeyNotFound)
		return
	}
	if !p.steppedUp(w, r, sess, existing, body.Proof) {
		return
	}
	deleted, err := p.store.DeletePasskey(r.Context(), sess.UserID, f["id"])
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonPasskeyDeleteFailed)
		return
	}
	if !deleted {
		writeAuthError(w, r, http.StatusNotFound, reasonPasskeyNotFound)
		return
	}
	p.mgr.emitSecurity(r.Context(), SecurityEvent{Kind: "passkey.deleted", UserID: sess.UserID, Remote: remoteHost(r)})
	w.WriteHeader(http.StatusNoContent)
}

// ─── step-up ──────────────────────────────────────────────────────────

// reauthOptionsHandler issues a step-up challenge limited to the
// signed-in account's own passkeys.
func (p *PasskeyPlugin) reauthOptionsHandler(w http.ResponseWriter, r *http.Request) {
	if !p.limit.guard(w, r) || !requireJSON(w, r) {
		return
	}
	sess, ok := p.requireFullSession(w, r)
	if !ok {
		return
	}
	creds, err := p.store.ListPasskeys(r.Context(), sess.UserID)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonPasskeyLookupFailed)
		return
	}
	if len(creds) == 0 {
		writeAuthError(w, r, http.StatusNotFound, reasonPasskeyNotFound)
		return
	}
	now := time.Now()
	var eligible []PasskeyCredential
	for _, c := range creds {
		if p.reauthEligible(c, sess, now) {
			eligible = append(eligible, c)
		}
	}
	creds = eligible
	if len(creds) == 0 {
		writeAuthError(w, r, http.StatusForbidden, reasonPasskeyTooNew)
		return
	}
	challenge, err := createPurposeToken(r.Context(), p.challenges, purposePasskeyReauth, sess.UserID, p.config.ChallengeTTL)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonChallengeCreateFailed)
		return
	}
	writeAuthJSON(w, r, http.StatusOK, map[string]any{"publicKey": map[string]any{
		"challenge":        b64url.EncodeToString([]byte(challenge)),
		"rpId":             p.rp.ID,
		"timeout":          p.config.ChallengeTTL.Milliseconds(),
		"userVerification": "required",
		"allowCredentials": credentialDescriptors(creds),
	}})
}

// reauthHandler verifies a step-up assertion by one of the signed-in
// account's passkeys and answers {"proof": "..."}, a single-use token
// the app's own handler spends with RedeemReauthProof.
func (p *PasskeyPlugin) reauthHandler(w http.ResponseWriter, r *http.Request) {
	if !p.limit.guard(w, r) {
		return
	}
	sess, ok := p.requireFullSession(w, r)
	if !ok {
		return
	}
	f, challenge, ok := p.readAssertion(w, r, "reauth", sess.UserID)
	if !ok {
		return
	}
	owner, err := redeemPurposeToken(r.Context(), p.challenges, purposePasskeyReauth, challenge)
	if err != nil || owner != sess.UserID {
		p.refuse(w, r, sess.UserID, "reauth", "challenge", errors.New("challenge unknown, spent, expired or another account's"))
		return
	}
	eligible := func(c PasskeyCredential) bool { return p.reauthEligible(c, sess, time.Now()) }
	if _, ok := p.checkAssertion(w, r, "reauth", f, sess.UserID, eligible); !ok {
		return
	}
	proof, err := createPurposeToken(r.Context(), p.challenges, purposePasskeyProof, proofPayload(sess), p.config.ReauthTTL)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonChallengeCreateFailed)
		return
	}
	p.mgr.emitSecurity(r.Context(), SecurityEvent{Kind: "passkey.reauthenticated", UserID: sess.UserID, Remote: remoteHost(r)})
	writeAuthJSON(w, r, http.StatusOK, map[string]any{"proof": proof})
}

// reauthEligible reports whether c may confirm a step-up for sess: it
// was registered before sess signed in, and at least ReauthMinAge ago.
// A session or passkey with no creation time is refused.
func (p *PasskeyPlugin) reauthEligible(c PasskeyCredential, sess *Session, now time.Time) bool {
	if c.CreatedAt.IsZero() || sess.CreatedAt.IsZero() {
		return false
	}
	// SQLPasskeyStore keeps whole seconds, so a passkey read back as x.0
	// may have been made at x.9: it predates the session only if its
	// whole second ended before the session began.
	if c.CreatedAt.Truncate(time.Second).Add(time.Second).After(sess.CreatedAt) {
		return false
	}
	return now.Sub(c.CreatedAt) >= p.config.ReauthMinAge
}

func (p *PasskeyPlugin) refuseTooNew(w http.ResponseWriter, r *http.Request, userID string) {
	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind: "passkey.refused", UserID: userID, Remote: remoteHost(r),
		Meta: map[string]string{"ceremony": "reauth", "reason": "too_new"},
	})
	writeAuthError(w, r, http.StatusForbidden, reasonPasskeyTooNew)
}

// proofPayload binds a step-up proof to the session that earned it: a
// digest of the account and the session token, so another session of
// the same account cannot spend it and the stored row names no one.
func proofPayload(sess *Session) string {
	return sha256hex(sess.UserID + "|" + sha256hex(sess.Token))
}

// steppedUp lets a passkey be added or removed. The gate is the
// account's, not the session's: once any of its passkeys is ReauthMinAge
// old (or has no creation time), the request carries a proof this
// session earned, whenever the session began, so a session that predates
// every passkey (the one that enrolled them, or one stolen before) cannot
// add or remove one either. A session that has no passkey able to confirm
// for it is told to sign in again with a passkey (403
// passkey_signin_required); one that has, to step up (403
// passkey_reauth_required). Before the first passkey is that old, the
// session suffices, and the owner was mailed when it was added.
func (p *PasskeyPlugin) steppedUp(w http.ResponseWriter, r *http.Request, sess *Session, creds []PasskeyCredential, proof string) bool {
	now := time.Now()
	gated := slices.ContainsFunc(creds, func(c PasskeyCredential) bool {
		return c.CreatedAt.IsZero() || now.Sub(c.CreatedAt) >= p.config.ReauthMinAge
	})
	if !gated {
		return true
	}
	if proof != "" {
		payload, err := redeemPurposeToken(r.Context(), p.challenges, purposePasskeyProof, proof)
		if err == nil && payload == proofPayload(sess) {
			return true
		}
	}
	if !slices.ContainsFunc(creds, func(c PasskeyCredential) bool { return p.reauthEligible(c, sess, now) }) {
		writeAuthError(w, r, http.StatusForbidden, reasonPasskeySignInAgain)
		return false
	}
	writeAuthError(w, r, http.StatusForbidden, reasonPasskeyReauthRequired)
	return false
}

// ErrReauthRequired is RedeemReauthProof's answer when the request has
// no full session or the proof is missing, spent, expired or another
// account's.
var ErrReauthRequired = errors.New("auth: passkey re-authentication required")

// RedeemReauthProof spends the proof a passed step-up returned
// (POST {base}/passkey/reauth), for the handler of a sensitive action.
// It answers nil only when r carries the full session the proof was
// issued to; the proof is spent either way, so it works once.
//
//	if err := passkeys.RedeemReauthProof(r, r.FormValue("passkey_reauth")); err != nil {
//		http.Error(w, "confirm with your passkey first", http.StatusForbidden)
//		return
//	}
func (p *PasskeyPlugin) RedeemReauthProof(r *http.Request, proof string) error {
	if p.mgr == nil || proof == "" {
		return ErrReauthRequired
	}
	payload, err := redeemPurposeToken(r.Context(), p.challenges, purposePasskeyProof, proof)
	if err != nil {
		return ErrReauthRequired
	}
	sess, _, _ := p.fullSession(r)
	if sess == nil || payload != proofPayload(sess) {
		return ErrReauthRequired
	}
	return nil
}

// purgeUserPasskeys removes every passkey a user has, for an account
// claim or a password reset: a passkey added by whoever held the
// account is a way back in.
func (m *AuthManager) purgeUserPasskeys(ctx context.Context, userID string) (int, error) {
	plugin, ok := m.Plugin("passkey")
	if !ok {
		return 0, nil
	}
	pp, ok := plugin.(*PasskeyPlugin)
	if !ok || pp.store == nil {
		return 0, nil
	}
	return pp.store.DeletePasskeysForUser(ctx, userID)
}
