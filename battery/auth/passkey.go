package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
	CreatedAt      time.Time
	LastUsedAt     time.Time
}

var (
	// ErrPasskeyNotFound is returned when no passkey has the id.
	ErrPasskeyNotFound = errors.New("auth: passkey not found")
	// ErrPasskeyExists is returned when a passkey id is already registered.
	ErrPasskeyExists = errors.New("auth: passkey already registered")
)

// PasskeyStore persists passkeys. Implementations must make AddPasskey
// refuse a duplicate id and RecordPasskeyUse a compare-and-swap.
type PasskeyStore interface {
	AddPasskey(ctx context.Context, c PasskeyCredential) error
	GetPasskey(ctx context.Context, id []byte) (PasskeyCredential, error)
	ListPasskeys(ctx context.Context, userID string) ([]PasskeyCredential, error)
	// RecordPasskeyUse moves the sign count from old to next and stamps
	// the use, only while the stored count is still old. It reports
	// whether it wrote: false means another sign-in got there first.
	RecordPasskeyUse(ctx context.Context, id []byte, old, next uint32, backedUp bool, at time.Time) (bool, error)
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
	return c
}

// AddPasskey implements PasskeyStore.
func (m *MemoryPasskeyStore) AddPasskey(_ context.Context, c PasskeyCredential) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.creds[string(c.ID)]; ok {
		return ErrPasskeyExists
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
//	POST {base}/passkey/delete            signed in; removes one
//
// Every endpoint speaks JSON (a cross-site page cannot send that without a
// CORS preflight the battery never answers). The browser half is the
// "passkey" runtime behaviour (passkey.js), bound to data-passkey-*
// buttons that stay hidden on a browser without WebAuthn.
type PasskeyPlugin struct {
	config     PasskeyConfig
	rp         webauthnRP
	mgr        *AuthManager
	store      PasskeyStore
	challenges MagicLinkTokenStore
	limit      *RateLimiter
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
	if cfg.RateLimit == nil {
		cfg.RateLimit = &RateLimiterConfig{MaxAttempts: 30, Window: time.Minute, BlockDuration: 5 * time.Minute}
	}
	return &PasskeyPlugin{
		config: cfg,
		rp:     webauthnRP{ID: cfg.RPID, Origins: slices.Clone(cfg.Origins)},
		limit:  newScopedRateLimiter(*cfg.RateLimit, "passkey"),
	}
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
	r.Post(basePath+"/passkey/login/options", http.HandlerFunc(p.loginOptionsHandler))
	r.Post(basePath+"/passkey/login", http.HandlerFunc(p.loginHandler))
	r.Post(basePath+"/passkey/register/options", http.HandlerFunc(p.registerOptionsHandler))
	r.Post(basePath+"/passkey/register", http.HandlerFunc(p.registerHandler))
	r.Get(basePath+"/passkey/list", http.HandlerFunc(p.listHandler))
	r.Post(basePath+"/passkey/delete", http.HandlerFunc(p.deleteHandler))
}

// maxUserHandleLen is WebAuthn's ceiling for user.id.
const maxUserHandleLen = 64

var b64url = base64.RawURLEncoding

// requireFullSession resolves a session that may manage the account's
// passkeys: signed in, not waiting on a second factor, and past the
// second factor when the account has one. Adding a passkey adds a way
// in, so a password-only session of a 2FA account must not do it.
func (p *PasskeyPlugin) requireFullSession(w http.ResponseWriter, r *http.Request) (*Session, bool) {
	sess, err := p.mgr.requestSession(r, false)
	if err != nil {
		writeAuthError(w, r, http.StatusUnauthorized, reasonNotAuthenticated)
		return nil, false
	}
	if sess.PendingTwoFactor {
		writeAuthError(w, r, http.StatusForbidden, reason2faRequired)
		return nil, false
	}
	if !sess.TwoFactorVerified {
		on, err := p.mgr.twoFactorEnabled(r.Context(), sess.UserID)
		if err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reason2faLookupFailed)
			return nil, false
		}
		if on {
			writeAuthError(w, r, http.StatusForbidden, reason2faRequired)
			return nil, false
		}
	}
	return sess, true
}

func writePasskeyJSON(w http.ResponseWriter, status int, v any) {
	writeCredentialHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
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
	challenge, err := createPurposeToken(r.Context(), p.challenges, purposePasskeyRegister, sess.UserID, p.config.ChallengeTTL)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonChallengeCreateFailed)
		return
	}
	exclude := make([]map[string]string, 0, len(existing))
	for _, c := range existing {
		exclude = append(exclude, map[string]string{"type": "public-key", "id": b64url.EncodeToString(c.ID)})
	}
	params := make([]map[string]any, 0, len(passkeyAlgs))
	for _, alg := range passkeyAlgs {
		params = append(params, map[string]any{"type": "public-key", "alg": alg})
	}
	writePasskeyJSON(w, http.StatusOK, map[string]any{"publicKey": map[string]any{
		"rp":                 map[string]string{"id": p.rp.ID, "name": p.config.RPName},
		"user":               map[string]string{"id": b64url.EncodeToString([]byte(sess.UserID)), "name": user.GetEmail(), "displayName": user.GetEmail()},
		"challenge":          b64url.EncodeToString([]byte(challenge)),
		"pubKeyCredParams":   params,
		"timeout":            p.config.ChallengeTTL.Milliseconds(),
		"attestation":        "none",
		"excludeCredentials": exclude,
		"authenticatorSelection": map[string]any{
			"residentKey": "required", "requireResidentKey": true, "userVerification": "required",
		},
	}})
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
		Name              string `json:"name"`
		ID                string `json:"id"`
		ClientDataJSON    string `json:"clientDataJSON"`
		AttestationObject string `json:"attestationObject"`
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
	owner, err := redeemPurposeToken(r.Context(), p.challenges, purposePasskeyRegister, challenge)
	if err != nil || owner != sess.UserID {
		p.refuse(w, r, sess.UserID, "register", "challenge", errors.New("challenge unknown, spent, expired or another account's"))
		return
	}
	res, err := p.rp.verifyAttestation(f["attestationObject"])
	if err != nil {
		p.refuse(w, r, sess.UserID, "register", "attestation", err)
		return
	}
	if string(res.credID) != string(f["id"]) {
		p.refuse(w, r, sess.UserID, "register", "credential_mismatch", errors.New("credential id does not match the attestation"))
		return
	}
	// The options call checked the cap, but challenges collected while
	// under it would each register past it; count again before storing.
	existing, err := p.store.ListPasskeys(r.Context(), sess.UserID)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonPasskeyLookupFailed)
		return
	}
	if len(existing) >= p.config.MaxPerUser {
		writeAuthError(w, r, http.StatusConflict, reasonPasskeyLimit)
		return
	}
	now := time.Now().UTC()
	cred := PasskeyCredential{
		ID: res.credID, UserID: sess.UserID, PublicKey: res.publicKey, Algorithm: res.alg,
		SignCount: res.signCount, Name: name, BackupEligible: res.backupEligible, BackedUp: res.backedUp,
		CreatedAt: now,
	}
	if err := p.store.AddPasskey(r.Context(), cred); err != nil {
		if errors.Is(err, ErrPasskeyExists) {
			writeAuthError(w, r, http.StatusConflict, reasonPasskeyAlreadyRegistered)
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
		Meta: map[string]string{"alg": strconv.FormatInt(res.alg, 10), "backup_eligible": strconv.FormatBool(res.backupEligible)},
	})
	writePasskeyJSON(w, http.StatusCreated, passkeyView(cred))
}

// ─── sign-in ──────────────────────────────────────────────────────────

func (p *PasskeyPlugin) loginOptionsHandler(w http.ResponseWriter, r *http.Request) {
	if !p.limit.guard(w, r) || !requireJSON(w, r) {
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
	writePasskeyJSON(w, http.StatusOK, map[string]any{"publicKey": map[string]any{
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
	var body struct {
		ID                string `json:"id"`
		ClientDataJSON    string `json:"clientDataJSON"`
		AuthenticatorData string `json:"authenticatorData"`
		Signature         string `json:"signature"`
		UserHandle        string `json:"userHandle"`
	}
	if !decodeJSONLimited(w, r, &body) {
		return
	}
	f, err := decodeB64Fields(map[string]string{
		"id": body.ID, "clientDataJSON": body.ClientDataJSON, "authenticatorData": body.AuthenticatorData,
		"signature": body.Signature, "userHandle": body.UserHandle,
	})
	if err != nil {
		writeAuthErrorFor(w, r, http.StatusBadRequest, err, i18n.StatusPhrase(http.StatusBadRequest))
		return
	}
	challenge, err := p.rp.checkClientData(f["clientDataJSON"], "webauthn.get")
	if err != nil {
		p.refuse(w, r, "", "login", "client_data", err)
		return
	}
	if _, err := redeemPurposeToken(r.Context(), p.challenges, purposePasskeyLogin, challenge); err != nil {
		p.refuse(w, r, "", "login", "challenge", errors.New("challenge unknown, spent or expired"))
		return
	}
	cred, err := p.store.GetPasskey(r.Context(), f["id"])
	if err != nil {
		if !errors.Is(err, ErrPasskeyNotFound) {
			writeAuthError(w, r, http.StatusInternalServerError, reasonPasskeyLookupFailed)
			return
		}
		p.refuse(w, r, "", "login", "unknown_credential", errors.New("unknown credential id"))
		return
	}
	// The authenticator names the account it holds the passkey for; it
	// must be the account the passkey was registered to.
	if string(f["userHandle"]) != cred.UserID {
		p.refuse(w, r, cred.UserID, "login", "user_handle", errors.New("user handle does not match the passkey's account"))
		return
	}
	res, err := p.rp.verifyAssertion(cred.PublicKey, f["clientDataJSON"], f["authenticatorData"], f["signature"])
	if err != nil {
		p.refuse(w, r, cred.UserID, "login", "signature", err)
		return
	}
	// Backup eligibility is fixed when the credential is made; a flip
	// means this is not the authenticator that registered it.
	if res.backupEligible != cred.BackupEligible {
		p.refuse(w, r, cred.UserID, "login", "backup_eligibility", errors.New("backup eligibility differs from registration"))
		return
	}
	// A counter that fails to move forward means two copies of the key
	// exist. Synced passkeys always report zero, which is no signal.
	if (res.signCount != 0 || cred.SignCount != 0) && res.signCount <= cred.SignCount {
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind: "passkey.clone_suspected", UserID: cred.UserID, Remote: remoteHost(r),
			Meta: map[string]string{"stored": strconv.FormatUint(uint64(cred.SignCount), 10), "presented": strconv.FormatUint(uint64(res.signCount), 10)},
		})
		writeAuthError(w, r, http.StatusUnauthorized, reasonPasskeySignIn)
		return
	}
	wrote, err := p.store.RecordPasskeyUse(r.Context(), cred.ID, cred.SignCount, res.signCount, res.backedUp, time.Now().UTC())
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonPasskeyUpdateFailed)
		return
	}
	if !wrote {
		writeAuthError(w, r, http.StatusConflict, reasonPasskeyConflict)
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
	writePasskeyJSON(w, http.StatusOK, out)
}

// refuse answers a failed ceremony with one fixed message and records
// why in the audit trail and the log.
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
	if ceremony == "login" {
		writeAuthError(w, r, http.StatusUnauthorized, reasonPasskeySignIn)
		return
	}
	writeAuthError(w, r, http.StatusBadRequest, reasonPasskeyRegistrationFailed)
}

// ─── management ───────────────────────────────────────────────────────

func passkeyView(c PasskeyCredential) map[string]any {
	v := map[string]any{
		"id": b64url.EncodeToString(c.ID), "name": c.Name,
		"createdAt": c.CreatedAt.UTC().Format(time.RFC3339), "backedUp": c.BackedUp,
	}
	if !c.LastUsedAt.IsZero() {
		v["lastUsedAt"] = c.LastUsedAt.UTC().Format(time.RFC3339)
	}
	return v
}

func (p *PasskeyPlugin) listHandler(w http.ResponseWriter, r *http.Request) {
	if !p.limit.guard(w, r) {
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
	writePasskeyJSON(w, http.StatusOK, map[string]any{"passkeys": out})
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
		ID string `json:"id"`
	}
	if !decodeJSONLimited(w, r, &body) {
		return
	}
	f, err := decodeB64Fields(map[string]string{"id": body.ID})
	if err != nil {
		writeAuthErrorFor(w, r, http.StatusBadRequest, err, i18n.StatusPhrase(http.StatusBadRequest))
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
