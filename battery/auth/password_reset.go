package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// PasswordSetter is the UserStore extension that stores a new password hash.
type PasswordSetter interface {
	SetPassword(ctx context.Context, userID, hashedPassword string) error
}

// PasswordSwapper sets newHash only while oldHash is stored, in one statement, and reports whether it wrote.
type PasswordSwapper interface {
	SwapPassword(ctx context.Context, userID, oldHash, newHash string) (bool, error)
}


// PasswordResetConfig configures the plugin.
type PasswordResetConfig struct {
	// BaseURL is the origin of the emailed reset link.
	BaseURL string

	// LinkPath is the same-origin path the reset link opens; default <BasePath>/reset-password.
	LinkPath string

	// TokenTTL is the reset link's lifetime; default one hour.
	TokenTTL time.Duration

	// EmailSender mails the reset link; nil sends nothing.
	EmailSender EmailSender

	// TokenStore keeps reset tokens; nil is an in-memory store, refused in production.
	TokenStore MagicLinkTokenStore

	// BodyTemplate builds the mail body from the reset URL; nil sends the URL alone.
	BodyTemplate func(ctx context.Context, url string) string

	// NoticeTemplate builds the notice a completed reset mails, given the providers it unlinked; nil uses the default text.
	NoticeTemplate func(ctx context.Context, unlinked []string) string

	// DevMode logs hashes of the address and token when EmailSender is nil.
	DevMode bool

	// RateLimit is the per-IP limit on both endpoints; nil means 10 a minute with a 15-minute block.
	RateLimit *RateLimiterConfig
}

// PasswordResetPlugin wires:
type PasswordResetPlugin struct {
	cfg      PasswordResetConfig
	mgr      *AuthManager
	store    MagicLinkTokenStore
	limit    *RateLimiter
	delivery *mailQueue
}

var _ AuthPluginOnStop = (*PasswordResetPlugin)(nil)

// NewPasswordResetPlugin builds the plugin with sensible defaults.
func NewPasswordResetPlugin(cfg PasswordResetConfig) *PasswordResetPlugin {
	if cfg.TokenTTL <= 0 {
		cfg.TokenTTL = time.Hour
	}
	store := cfg.TokenStore
	if store == nil {
		store = NewMemoryMagicLinkTokenStore()
	}
	if cfg.RateLimit == nil {
		cfg.RateLimit = &RateLimiterConfig{
			MaxAttempts:   10,
			Window:        time.Minute,
			BlockDuration: 15 * time.Minute,
		}
	}
	p := &PasswordResetPlugin{
		cfg:      cfg,
		store:    store,
		limit:    newScopedRateLimiter(*cfg.RateLimit, "password_reset"),
		delivery: newMailQueue("password-reset"),
	}
	return p
}

func (p *PasswordResetPlugin) Name() string { return "password-reset" }

func (p *PasswordResetPlugin) Init(mgr *AuthManager) error {
	if why := invalidLinkBaseURL(p.cfg.BaseURL); why != "" {
		return fmt.Errorf("auth: password-reset plugin: BaseURL %q %s", p.cfg.BaseURL, why)
	}
	if p.cfg.LinkPath != "" && (!handler.IsSafeRelativePath(p.cfg.LinkPath) || strings.ContainsAny(p.cfg.LinkPath, "?#")) {
		return fmt.Errorf("auth: password-reset plugin: LinkPath %q is not a same-origin path with no query (one leading \"/\", no scheme or host)", p.cfg.LinkPath)
	}
	if p.cfg.TokenStore == nil {
		if err := refuseInMemoryTokenStore(mgr, "password-reset"); err != nil {
			return err
		}
	}
	p.mgr = mgr
	return nil
}

func (p *PasswordResetPlugin) RegisterRoutes(r *router.Router, basePath string) {
	r = r.Group("", browserAccountOnly)
	r.Post(basePath+"/forgot-password", http.HandlerFunc(p.forgotHandler))
	r.Post(basePath+"/reset-password", http.HandlerFunc(p.resetHandler))
}

func (p *PasswordResetPlugin) burnUnknownBranchWork(r *http.Request) {
	if _, err := createPurposeToken(r.Context(), p.store, purposeReset, "\x00no-account", p.cfg.TokenTTL); err != nil {
		slog.Warn("password-reset decoy token mint failed",
			"plugin", "password-reset", "err", err)
	}
}

func (p *PasswordResetPlugin) forgotHandler(w http.ResponseWriter, r *http.Request) {
	if rejectCrossSiteForm(w, r) {
		return
	}
	if !guardAuthLimit(p.limit, w, r) {
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	isForm := isFormRequest(r)
	if isForm {
		v, ok := decodeFormFields(w, r, "email")
		if !ok {
			return
		}
		body.Email = v["email"]
	} else if !decodeJSONLimited(w, r, &body) {
		return
	}
	{
		ce, cerr := p.mgr.canonicalizeEmail(body.Email)
		if cerr != nil {
			writeAuthErrorFor(w, r, http.StatusBadRequest, cerr, reasonComposedEmail)
			return
		}
		body.Email = ce
	}
	if !emailWithinLimit(w, r, body.Email) {
		return
	}

	defer func() {
		if isForm {
			http.Redirect(w, r, successRedirect(w, r, "/"), http.StatusSeeOther)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sent": true})
	}()

	store := p.mgr.UserStore()
	if store == nil {
		return
	}
	if !p.mgr.allowMailAddress(r.Context(), body.Email) {
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind:   "password.reset_requested",
			Email:  body.Email,
			Remote: remoteHost(r),
			Meta:   map[string]string{"throttled": "address"},
		})
		return
	}
	user, _, err := store.FindByEmail(r.Context(), body.Email)
	if err != nil {
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind:   "password.reset_requested",
			Email:  body.Email,
			Remote: remoteHost(r),
			Meta:   map[string]string{"known": "false"},
		})
		p.burnUnknownBranchWork(r)
		return
	}

	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind:   "password.reset_requested",
		UserID: user.GetID(),
		Email:  body.Email,
		Remote: remoteHost(r),
		Meta:   map[string]string{"known": "true"},
	})

	tok, err := createPurposeToken(r.Context(), p.store, purposeReset, user.GetID(), p.cfg.TokenTTL)
	if err != nil {
		return
	}
	linkPath := p.cfg.LinkPath
	if linkPath == "" {
		linkPath = p.mgr.Config().BasePath + "/reset-password"
	}
	resetURL := fmt.Sprintf("%s%s?token=%s", p.cfg.BaseURL, linkPath, tok)

	switch {
	case p.cfg.EmailSender != nil:
		p.queueResetEmail(r.Context(), user.GetEmail(), resetURL)
	case p.cfg.DevMode:
		slog.Info("password-reset dev",
			"plugin", "password-reset",
			"email_hash", hashedIdentifier(user.GetEmail()),
			"token_hash", hashedIdentifier(tok))
	default:
	}
}

func (p *PasswordResetPlugin) queueResetEmail(requestCtx context.Context, to, resetURL string) {
	p.delivery.enqueue(requestCtx, to, func(ctx context.Context) {
		body := resetURL
		if p.cfg.BodyTemplate != nil {
			body = p.cfg.BodyTemplate(ctx, resetURL)
		}
		if err := p.cfg.EmailSender.Send(ctx, to, body); err != nil {
			slog.Warn("password-reset email send failed",
				"plugin", "password-reset",
				"email_hash", hashedIdentifier(to),
				"err", err)
		}
	})
}

// OnStop stops the mail queue; ctx bounds the wait.
func (p *PasswordResetPlugin) OnStop(ctx context.Context) error {
	return p.delivery.stop(ctx)
}

func (p *PasswordResetPlugin) dropLinks(ctx context.Context, userID, reason, remote string) {
	n, supported, err := purgePurposeTokens(ctx, p.store, purposeReset, userID)
	switch {
	case err != nil:
		slog.Warn("password-reset sibling token purge failed",
			"plugin", "password-reset", "reason", reason, "user_hash", hashedIdentifier(userID), "err", err)
	case !supported:
		slog.Warn("password-reset could not drop sibling reset tokens: token store does not implement MagicLinkTokenPurger",
			"plugin", "password-reset", "reason", reason, "user_hash", hashedIdentifier(userID))
	case n > 0:
		p.mgr.emitSecurity(ctx, SecurityEvent{
			Kind:   "reset_tokens.purged",
			UserID: userID,
			Remote: remote,
			Meta:   map[string]string{"reason": reason, "count": strconv.Itoa(n)},
		})
	}
}

func (p *PasswordResetPlugin) resetHandler(w http.ResponseWriter, r *http.Request) {
	if rejectCrossSiteForm(w, r) {
		return
	}
	if !guardAuthLimit(p.limit, w, r) {
		return
	}
	var body struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	isForm := isFormRequest(r)
	if isForm {
		v, ok := decodeFormFields(w, r, "token", "password")
		if !ok {
			return
		}
		body.Token, body.Password = v["token"], v["password"]
	} else if !decodeJSONLimited(w, r, &body) {
		return
	}
	fail := func(status int, reason i18n.Phrase) {
		if isForm {
			writeFormAuthError(w, r, status, reason)
			return
		}
		writeAuthError(w, r, status, reason)
	}
	if body.Token == "" || body.Password == "" {
		fail(http.StatusBadRequest, reasonTokenAndPasswordRequired)
		return
	}
	if len(body.Password) > 128 {
		fail(http.StatusBadRequest, reasonPasswordTooLong)
		return
	}

	setter, ok := p.mgr.UserStore().(PasswordSetter)
	if !ok {
		fail(http.StatusInternalServerError, reasonStoreNoPasswordSetter)
		return
	}

	if err := ValidatePasswordStrength(body.Password); err != nil {
		fail(http.StatusBadRequest, reasonPasswordTooShort)
		return
	}

	hash, err := HashPassword(body.Password)
	if err != nil {
		fail(http.StatusInternalServerError, reasonHashFailed)
		return
	}

	userID, err := redeemPurposeToken(r.Context(), p.store, purposeReset, body.Token)
	if err != nil {
		fail(http.StatusUnauthorized, reasonInvalidToken)
		return
	}
	if err := setter.SetPassword(r.Context(), userID, hash); err != nil {
		if errors.Is(err, ErrUserNotFound) {
			fail(http.StatusNotFound, reasonUserNotFound)
			return
		}
		fail(http.StatusInternalServerError, reasonSetPasswordFailed)
		return
	}

	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind:   "password.reset_completed",
		UserID: userID,
		Remote: remoteHost(r),
	})

	p.dropLinks(r.Context(), userID, "password_reset", remoteHost(r))
	incomplete := false
	if n, err := p.mgr.purgeUserPasskeys(r.Context(), userID); err != nil {
		slog.Warn("password-reset passkey removal failed",
			"plugin", "password-reset", "user_hash", hashedIdentifier(userID), "err", err)
		incomplete = true
	} else if n > 0 {
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind:   "passkey.deleted",
			UserID: userID,
			Remote: remoteHost(r),
			Meta:   map[string]string{"reason": "password_reset", "count": strconv.Itoa(n)},
		})
	}
	var unlinked []string
	lister, unlinker, err := oauthUnlinkTools(p.mgr.UserStore())
	if err == nil {
		unlinked, err = p.mgr.unlinkAllOAuth(r.Context(), userID, lister, unlinker)
	}
	for _, provider := range unlinked {
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind:   "oauth.unlinked",
			UserID: userID,
			Remote: remoteHost(r),
			Meta:   map[string]string{"provider": provider, "reason": "password_reset"},
		})
	}
	if err != nil {
		slog.Warn("password-reset OAuth unlink failed",
			"plugin", "password-reset", "user_hash", hashedIdentifier(userID), "err", err)
		incomplete = true
	}
	if err := p.mgr.revokeUserSessions(r.Context(), userID, "password_reset", remoteHost(r)); err != nil {
		incomplete = true
	}
	if err := p.mgr.dropMagicLinks(r.Context(), userID, "password_reset", remoteHost(r)); err != nil {
		slog.Warn("password-reset magic-link purge failed",
			"plugin", "password-reset", "user_hash", hashedIdentifier(userID), "err", err)
		incomplete = true
	}
	if n, supported, err := p.mgr.revokeUserAPITokens(r.Context(), userID); err != nil {
		slog.Warn("password-reset API token revocation failed",
			"plugin", "password-reset", "user_hash", hashedIdentifier(userID), "err", err)
		incomplete = true
	} else if supported && n > 0 {
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind:   "token.revoked",
			UserID: userID,
			Remote: remoteHost(r),
			Meta:   map[string]string{"reason": "password_reset", "count": strconv.Itoa(n)},
		})
	}
	if incomplete {
		fail(http.StatusInternalServerError, reasonResetIncomplete)
		return
	}
	if checker, ok := p.mgr.UserStore().(EmailVerifiedChecker); ok {
		verified, err := checker.IsEmailVerified(r.Context(), userID)
		if err == nil && !verified {
			err = p.mgr.claimAccount(r.Context(), userID, "password_reset", remoteHost(r), false)
		}
		if err != nil {
			slog.Warn("password-reset account claim failed",
				"plugin", "password-reset", "user_hash", hashedIdentifier(userID), "err", err)
			fail(http.StatusInternalServerError, reasonResetIncomplete)
			return
		}
	}

	p.queueResetNotice(r.Context(), userID, unlinked)

	if isForm {
		http.Redirect(w, r, successRedirect(w, r, "/"), http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"updated": true})
}

func (p *PasswordResetPlugin) queueResetNotice(requestCtx context.Context, userID string, unlinked []string) {
	if p.cfg.EmailSender == nil {
		return
	}
	user, err := p.mgr.UserStore().FindByID(requestCtx, userID)
	if err != nil || user == nil || user.GetEmail() == "" {
		slog.Warn("password-reset notice skipped: no address for the account",
			"plugin", "password-reset", "user_hash", hashedIdentifier(userID), "err", err)
		return
	}
	to := user.GetEmail()
	p.delivery.enqueue(requestCtx, to, func(ctx context.Context) {
		var body string
		if p.cfg.NoticeTemplate != nil {
			body = p.cfg.NoticeTemplate(ctx, unlinked)
		} else {
			body = defaultResetNotice(ctx, unlinked)
		}
		if err := p.cfg.EmailSender.Send(ctx, to, body); err != nil {
			slog.Warn("password-reset notice send failed",
				"plugin", "password-reset", "email_hash", hashedIdentifier(to), "err", err)
		}
	})
}

func defaultResetNotice(ctx context.Context, unlinked []string) string {
	parts := []string{resetNoticeDone.Text(ctx)}
	if len(unlinked) > 0 {
		parts = append(parts, resetNoticeUnlinked.Text(ctx, map[string]any{"providers": strings.Join(unlinked, ", ")}))
	}
	parts = append(parts, resetNoticeWarning.Text(ctx))
	return strings.Join(parts, "\n\n")
}
