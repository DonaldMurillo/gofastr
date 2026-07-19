package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// recordingSink is a thread-safe AuditSink that captures every event for
// assertions. It is the test double for the SQL sink — the SQL sink itself
// is exercised separately against an in-memory SQLite DB.
type recordingSink struct {
	mu     sync.Mutex
	events []SecurityEvent
}

func (s *recordingSink) SecurityEvent(_ context.Context, ev SecurityEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func (s *recordingSink) snapshot() []SecurityEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SecurityEvent, len(s.events))
	copy(out, s.events)
	return out
}

func (s *recordingSink) kinds() []string {
	evs := s.snapshot()
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.Kind
	}
	return out
}

func (s *recordingSink) findByKind(kind string) *SecurityEvent {
	for _, e := range s.snapshot() {
		if e.Kind == kind {
			ev := e
			return &ev
		}
	}
	return nil
}

func (s *recordingSink) findByKindAll(kind string) []SecurityEvent {
	var out []SecurityEvent
	for _, e := range s.snapshot() {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// panickingSink wraps a recordingSink but panics on every call, to prove
// emitSecurity isolates auth flows from a misbehaving sink.
type panickingSink struct{ rec *recordingSink }

func (p *panickingSink) SecurityEvent(ctx context.Context, ev SecurityEvent) {
	// Still record first so a test can confirm the event was reached, then
	// blow up to exercise the recover path.
	p.rec.SecurityEvent(ctx, ev)
	panic("synthetic sink failure")
}

// ─── harness ───────────────────────────────────────────────────────────

type auditFixture struct {
	mgr      *AuthManager
	store    *userStoreWithPassword
	router   *router.Router
	rec      *recordingSink
	pwSender *stubEmailSender
	mlSender *mockEmailSender
}

func buildAuditManager(t *testing.T, sink AuditSink) (*AuthManager, *userStoreWithPassword, *router.Router, *stubEmailSender, *mockEmailSender) {
	t.Helper()
	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{
		JWTSecret:     "test-secret",
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
		DevMode:       true,
		AuditSink:     sink,
	})
	pwSender := &stubEmailSender{}
	mlSender := &mockEmailSender{}
	mgr.Use(NewCorePlugin())
	mgr.Use(NewTwoFAPlugin(TwoFAConfig{}))
	mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{
		BaseURL:     "http://localhost",
		TokenTTL:    time.Hour,
		EmailSender: pwSender,
	}))
	mgr.Use(NewMagicLinkPlugin(MagicLinkConfig{
		BaseURL:      "http://localhost",
		OnSuccessURL: "/",
		TokenTTL:     time.Hour,
		EmailSender:  mlSender,
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	return mgr, store, r, pwSender, mlSender
}

// auditHarness wires every plugin with a fresh recordingSink.
func auditHarness(t *testing.T) *auditFixture {
	t.Helper()
	rec := &recordingSink{}
	mgr, store, r, pw, ml := buildAuditManager(t, rec)
	return &auditFixture{mgr: mgr, store: store, router: r, rec: rec, pwSender: pw, mlSender: ml}
}

func (f *auditFixture) seedUser(t *testing.T, id, email, password string) {
	t.Helper()
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	u := &BasicUser{ID: id, Email: email, Roles: []string{"user"}}
	f.store.users[email] = &storeEntry{user: u, hash: hash}
	f.store.byID[id] = f.store.users[email]
}

// cookieJar folds Set-Cookie responses back into subsequent requests so
// multi-step flows (login → 2FA) reuse the session cookie.
type cookieJar struct{ cookies []*http.Cookie }

func (j *cookieJar) do(r *router.Router, method, path string, body any, remote string) *httptest.ResponseRecorder {
	var rd *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	var req *http.Request
	if rd != nil {
		req = httptest.NewRequest(method, path, rd)
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if remote != "" {
		req.RemoteAddr = remote
	}
	for _, c := range j.cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		j.cookies = append(j.cookies, c)
	}
	return rec
}

func (j *cookieJar) sessionCookieValue() string {
	for i := len(j.cookies) - 1; i >= 0; i-- {
		if j.cookies[i].Name == "session_id" && j.cookies[i].Value != "" {
			return j.cookies[i].Value
		}
	}
	return ""
}

func mustStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, want, rec.Body.String())
	}
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode body: %v (body=%s)", err, rec.Body.String())
	}
	return m
}

// ─── login ─────────────────────────────────────────────────────────────

func TestAudit_LoginSuccess(t *testing.T) {
	f := auditHarness(t)
	f.seedUser(t, "u-login", "login@example.com", "supersecret1")

	jar := &cookieJar{}
	rec := jar.do(f.router, http.MethodPost, "/auth/login",
		map[string]string{"email": "login@example.com", "password": "supersecret1"}, "203.0.113.9:5555")
	mustStatus(t, rec, http.StatusOK)

	ev := f.rec.findByKind("login.succeeded")
	if ev == nil {
		t.Fatalf("no login.succeeded; got %v", f.rec.kinds())
	}
	if ev.UserID != "u-login" {
		t.Errorf("UserID = %q, want u-login", ev.UserID)
	}
	if ev.Email != "login@example.com" {
		t.Errorf("Email = %q", ev.Email)
	}
	if ev.Remote != "203.0.113.9" {
		t.Errorf("Remote = %q, want 203.0.113.9", ev.Remote)
	}
}

func TestAudit_LoginFailedWrongPassword(t *testing.T) {
	f := auditHarness(t)
	f.seedUser(t, "u-wp", "wp@example.com", "correctpw1")

	jar := &cookieJar{}
	rec := jar.do(f.router, http.MethodPost, "/auth/login",
		map[string]string{"email": "wp@example.com", "password": "WRONGpassword1"}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	ev := f.rec.findByKind("login.failed")
	if ev == nil {
		t.Fatalf("no login.failed; got %v", f.rec.kinds())
	}
	if ev.UserID != "u-wp" {
		t.Errorf("UserID = %q, want u-wp (known user)", ev.UserID)
	}
	if ev.Meta["reason"] != "bad_credentials" {
		t.Errorf("reason = %q, want bad_credentials", ev.Meta["reason"])
	}
}

func TestAudit_LoginFailedUnknownEmail(t *testing.T) {
	f := auditHarness(t)
	jar := &cookieJar{}
	rec := jar.do(f.router, http.MethodPost, "/auth/login",
		map[string]string{"email": "nobody@example.com", "password": "anypassword1"}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	ev := f.rec.findByKind("login.failed")
	if ev == nil {
		t.Fatalf("no login.failed; got %v", f.rec.kinds())
	}
	if ev.UserID != "" {
		t.Errorf("UserID = %q, want empty for unknown email", ev.UserID)
	}
	if ev.Meta["reason"] != "bad_credentials" {
		t.Errorf("reason = %q, want bad_credentials", ev.Meta["reason"])
	}
}

// ─── logout ────────────────────────────────────────────────────────────

func TestAudit_LogoutRevokesSession(t *testing.T) {
	f := auditHarness(t)
	f.seedUser(t, "u-out", "out@example.com", "supersecret1")

	jar := &cookieJar{}
	jar.do(f.router, http.MethodPost, "/auth/login",
		map[string]string{"email": "out@example.com", "password": "supersecret1"}, "")
	rec := jar.do(f.router, http.MethodPost, "/auth/logout", nil, "")
	mustStatus(t, rec, http.StatusNoContent)

	ev := f.rec.findByKind("session.revoked")
	if ev == nil {
		t.Fatalf("no session.revoked; got %v", f.rec.kinds())
	}
	if ev.UserID != "u-out" {
		t.Errorf("UserID = %q, want u-out", ev.UserID)
	}
	if ev.Meta["reason"] != "logout" {
		t.Errorf("reason = %q, want logout", ev.Meta["reason"])
	}
}

// ─── register ──────────────────────────────────────────────────────────

func TestAudit_RegisterSucceeded(t *testing.T) {
	f := auditHarness(t)
	jar := &cookieJar{}
	rec := jar.do(f.router, http.MethodPost, "/auth/register",
		map[string]string{"email": "new@example.com", "password": "newpassword1"}, "")
	mustStatus(t, rec, http.StatusCreated)

	ev := f.rec.findByKind("register.succeeded")
	if ev == nil {
		t.Fatalf("no register.succeeded; got %v", f.rec.kinds())
	}
	if ev.Email != "new@example.com" {
		t.Errorf("Email = %q", ev.Email)
	}
	if ev.UserID == "" {
		t.Errorf("UserID empty; expected the new user id")
	}
}

// ─── 2FA lifecycle ─────────────────────────────────────────────────────

func TestAudit_TwoFALifecycle(t *testing.T) {
	f := auditHarness(t)
	f.seedUser(t, "u-2fa", "2fa@example.com", "supersecret1")

	jar := &cookieJar{}
	jar.do(f.router, http.MethodPost, "/auth/login",
		map[string]string{"email": "2fa@example.com", "password": "supersecret1"}, "")

	// Enroll → returns the TOTP secret.
	enroll := jar.do(f.router, http.MethodPost, "/auth/2fa/enroll", nil, "")
	mustStatus(t, enroll, http.StatusOK)
	secret := decodeBody(t, enroll)["secret"].(string)

	// Verify with a valid code → 2fa.enrolled.
	step := uint64(time.Now().Unix()) / 30
	jar.do(f.router, http.MethodPost, "/auth/2fa/verify",
		map[string]string{"code": GenerateTOTP(secret, step)}, "")

	// Challenge with a valid code → 2fa.challenge_succeeded.
	jar.do(f.router, http.MethodPost, "/auth/2fa/challenge",
		map[string]string{"code": GenerateTOTP(secret, step)}, "")

	// Challenge with a bad code → 2fa.challenge_failed.
	bad := jar.do(f.router, http.MethodPost, "/auth/2fa/challenge",
		map[string]string{"code": "000000"}, "")
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("bad challenge status = %d, want 401", bad.Code)
	}

	// Disable → 2fa.disabled.
	jar.do(f.router, http.MethodPost, "/auth/2fa/disable", nil, "")

	got := f.rec.kinds()
	want := []string{"login.succeeded", "2fa.enrolled", "2fa.challenge_succeeded", "2fa.challenge_failed", "2fa.disabled"}
	if len(got) < len(want) {
		t.Fatalf("events = %v, want at least %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("event[%d] = %q, want %q (full: %v)", i, got[i], w, got)
		}
	}
}

// ─── password reset ────────────────────────────────────────────────────

func TestAudit_PasswordResetFlow(t *testing.T) {
	f := auditHarness(t)
	f.seedUser(t, "u-reset", "reset@example.com", "oldpassword1")

	jar := &cookieJar{}
	// Known email.
	jar.do(f.router, http.MethodPost, "/auth/forgot-password",
		map[string]string{"email": "reset@example.com"}, "")
	// Unknown email.
	jar.do(f.router, http.MethodPost, "/auth/forgot-password",
		map[string]string{"email": "ghost@example.com"}, "")

	reqs := f.rec.findByKindAll("password.reset_requested")
	if len(reqs) != 2 {
		t.Fatalf("expected 2 reset_requested, got %d (%v)", len(reqs), f.rec.kinds())
	}
	var known, unknown *SecurityEvent
	for i := range reqs {
		if reqs[i].UserID != "" {
			known = &reqs[i]
		} else {
			unknown = &reqs[i]
		}
	}
	if known == nil || known.UserID != "u-reset" {
		t.Errorf("known reset event missing/wrong: %+v", known)
	}
	if unknown == nil || unknown.Email != "ghost@example.com" {
		t.Errorf("unknown reset event missing/wrong: %+v", unknown)
	}

	// Complete the reset for the known user.
	_, body := f.pwSender.snapshot()
	tok := extractTokenFromBody(body)
	if tok == "" {
		t.Fatalf("no reset token in email body: %q", body)
	}
	rec := jar.do(f.router, http.MethodPost, "/auth/reset-password",
		map[string]string{"token": tok, "password": "brandnewpw1"}, "")
	mustStatus(t, rec, http.StatusOK)

	if ev := f.rec.findByKind("password.reset_completed"); ev == nil || ev.UserID != "u-reset" {
		t.Errorf("reset_completed missing/wrong: %+v", f.rec.findByKind("password.reset_completed"))
	}
	// Session revocation on reset — the memory store implements SessionUserPurger.
	rev := f.rec.findByKind("session.revoked")
	if rev == nil {
		t.Fatalf("no session.revoked from reset; got %v", f.rec.kinds())
	}
	if rev.Meta["reason"] != "password_reset" {
		t.Errorf("reason = %q, want password_reset", rev.Meta["reason"])
	}
}

// ─── magic link ────────────────────────────────────────────────────────

func TestAudit_MagicLinkFlow(t *testing.T) {
	f := auditHarness(t)
	jar := &cookieJar{}

	send := jar.do(f.router, http.MethodPost, "/auth/magic-link/send",
		map[string]string{"email": "magic@example.com"}, "")
	mustStatus(t, send, http.StatusOK)

	if ev := f.rec.findByKind("magiclink.requested"); ev == nil || ev.Email != "magic@example.com" {
		t.Errorf("magiclink.requested missing/wrong: %+v", f.rec.findByKind("magiclink.requested"))
	}

	// Consume the token from the captured magic-link URL.
	tok := extractTokenFromBody(f.mlSender.lastURL)
	if tok == "" {
		t.Fatalf("no token in magic link URL: %q", f.mlSender.lastURL)
	}
	// The verify endpoint is a GET with the token as a query param.
	req := httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token="+tok, nil)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("magic-link verify status = %d, want 302 (body=%s)", rec.Code, rec.Body.String())
	}
	if ev := f.rec.findByKind("magiclink.consumed"); ev == nil {
		t.Errorf("magiclink.consumed missing; got %v", f.rec.kinds())
	} else if ev.Email != "magic@example.com" {
		t.Errorf("consumed Email = %q, want magic@example.com", ev.Email)
	}
}

// ─── nil sink ──────────────────────────────────────────────────────────

func TestAudit_NilSinkNoPanic(t *testing.T) {
	// AuthConfig.AuditSink left nil — flows must work unchanged.
	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{
		JWTSecret: "test-secret", SessionTTL: time.Hour,
		SessionCookie: "session_id", UserStore: store, DevMode: true,
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	store.users["nil@example.com"] = &storeEntry{
		user: &BasicUser{ID: "u-nil", Email: "nil@example.com", Roles: []string{"user"}},
		hash: mustHash(t, "supersecret1"),
	}
	store.byID["u-nil"] = store.users["nil@example.com"]

	body, _ := json.Marshal(map[string]string{"email": "nil@example.com", "password": "supersecret1"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	mustStatus(t, rec, http.StatusOK)
}

func mustHash(t *testing.T, pw string) string {
	t.Helper()
	h, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	return h
}

// ─── panicking sink ────────────────────────────────────────────────────

func TestAudit_PanickingSinkRecovered(t *testing.T) {
	inner := &recordingSink{}
	_, store, r, _, _ := buildAuditManager(t, &panickingSink{rec: inner})
	store.users["panic@example.com"] = &storeEntry{
		user: &BasicUser{ID: "u-panic", Email: "panic@example.com", Roles: []string{"user"}},
		hash: mustHash(t, "supersecret1"),
	}
	store.byID["u-panic"] = store.users["panic@example.com"]

	jar := &cookieJar{}
	rec := jar.do(r, http.MethodPost, "/auth/login",
		map[string]string{"email": "panic@example.com", "password": "supersecret1"}, "")
	// Login must still succeed despite the sink panicking.
	mustStatus(t, rec, http.StatusOK)
	// The event reached the sink (inner recorded) before the panic.
	if ev := inner.findByKind("login.succeeded"); ev == nil {
		t.Errorf("expected login.succeeded to reach the sink before panic")
	}
}

// ─── secret-leak guard ─────────────────────────────────────────────────

// TestAudit_NoSecretsLeak runs a login + 2FA + password-reset flow using
// distinctive secret strings, then asserts that NONE of them appear anywhere
// in the marshalled audit events. The only user-controlled string allowed in
// any event is Email.
func TestAudit_NoSecretsLeak(t *testing.T) {
	f := auditHarness(t)
	const (
		loginPw   = "hunter2-XYZZY-leak"
		resetPw   = "freshpw-PLUGH-leak"
		userEmail = "leak@example.com"
	)
	f.seedUser(t, "u-leak", userEmail, loginPw)

	jar := &cookieJar{}
	// Login → session cookie value is a secret.
	jar.do(f.router, http.MethodPost, "/auth/login",
		map[string]string{"email": userEmail, "password": loginPw}, "")
	sessionToken := jar.sessionCookieValue()
	if sessionToken == "" {
		t.Fatal("no session cookie captured")
	}

	// 2FA enroll → TOTP secret + backup codes are secrets.
	enroll := jar.do(f.router, http.MethodPost, "/auth/2fa/enroll", nil, "")
	totpSecret := decodeBody(t, enroll)["secret"].(string)
	step := uint64(time.Now().Unix()) / 30
	verify := jar.do(f.router, http.MethodPost, "/auth/2fa/verify",
		map[string]string{"code": GenerateTOTP(totpSecret, step)}, "")
	backupCodesAny, _ := decodeBody(t, verify)["backup_codes"].([]any)
	var backupCodes []string
	for _, c := range backupCodesAny {
		if s, ok := c.(string); ok {
			backupCodes = append(backupCodes, s)
		}
	}

	// Password reset → reset token is a secret.
	jar.do(f.router, http.MethodPost, "/auth/forgot-password",
		map[string]string{"email": userEmail}, "")
	_, emailBody := f.pwSender.snapshot()
	resetTok := extractTokenFromBody(emailBody)
	jar.do(f.router, http.MethodPost, "/auth/reset-password",
		map[string]string{"token": resetTok, "password": resetPw}, "")

	secrets := []string{
		loginPw, resetPw, sessionToken, totpSecret, resetTok,
	}
	secrets = append(secrets, backupCodes...)

	// Marshal EVERY recorded event and grep for each secret.
	for i, ev := range f.rec.snapshot() {
		b, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal event %d: %v", i, err)
		}
		for _, secret := range secrets {
			if secret == "" {
				continue
			}
			if strings.Contains(string(b), secret) {
				t.Errorf("SECRET LEAK: %q found in event %d (%s): %s",
					secret, i, ev.Kind, b)
			}
		}
	}
}

// ─── SQL sink against SQLite ───────────────────────────────────────────

func TestSQLAuditSink_WritesRow(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	sink, err := NewSQLAuditSink(db, "")
	if err != nil {
		t.Fatalf("NewSQLAuditSink: %v", err)
	}
	sink.SecurityEvent(context.Background(), SecurityEvent{
		Kind:   "login.failed",
		UserID: "u-sql",
		Email:  "sql@example.com",
		Remote: "198.51.100.7",
		Meta:   map[string]string{"reason": "bad_credentials"},
	})

	row := db.QueryRow(`SELECT entity, op, record_id, actor_id, diff FROM audit_log LIMIT 1`)
	var entity, op, recordID, actorID, diff string
	if err := row.Scan(&entity, &op, &recordID, &actorID, &diff); err != nil {
		t.Fatalf("scan audit row: %v", err)
	}
	if entity != "auth" {
		t.Errorf("entity = %q, want auth", entity)
	}
	if op != "login.failed" {
		t.Errorf("op = %q, want login.failed", op)
	}
	if recordID != "u-sql" {
		t.Errorf("record_id = %q, want u-sql", recordID)
	}
	if actorID != "u-sql" {
		t.Errorf("actor_id = %q, want u-sql", actorID)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(diff), &d); err != nil {
		t.Fatalf("diff decode: %v", err)
	}
	if d["email"] != "sql@example.com" {
		t.Errorf("diff.email = %v", d["email"])
	}
	if d["remote"] != "198.51.100.7" {
		t.Errorf("diff.remote = %v", d["remote"])
	}
	if d["reason"] != "bad_credentials" {
		t.Errorf("diff.reason = %v", d["reason"])
	}
}

func TestSQLAuditSink_EmptyUserIDRecordDash(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	sink, err := NewSQLAuditSink(db, "audit_log")
	if err != nil {
		t.Fatalf("NewSQLAuditSink: %v", err)
	}
	// Empty UserID (failed login for unknown account) → record_id "-".
	sink.SecurityEvent(context.Background(), SecurityEvent{
		Kind:  "login.failed",
		Email: "ghost@example.com",
	})
	var recordID string
	if err := db.QueryRow(`SELECT record_id FROM audit_log LIMIT 1`).Scan(&recordID); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if recordID != "-" {
		t.Errorf("record_id = %q, want '-' for empty UserID", recordID)
	}
}

// ─── OAuth linked / login / refused ────────────────────────────────────

func TestAudit_OAuthLinkedLoginRefused(t *testing.T) {
	store := newLinkingUserStore()
	rec := &recordingSink{}
	mgr := New(AuthConfig{
		JWTSecret: "test-secret", SessionTTL: time.Hour,
		SessionCookie: "session_id", UserStore: store, DevMode: true, AuditSink: rec,
	})
	prov := &stubOAuthProvider{
		name:     "stub",
		userInfo: &OAuth2UserInfo{ID: "ext-1", Email: "first@example.com", Provider: "stub"},
	}
	mgr.Use(NewOAuth2Plugin(OAuth2Config{
		Providers:   map[string]OAuth2Provider{"stub": prov},
		StateSecret: "test-secret",
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)

	// First callback: auto-create + link → oauth.linked.
	w1 := runCallback(t, mgr, r, "stub")
	if w1.Code != http.StatusFound {
		t.Fatalf("first callback: %d", w1.Code)
	}
	if ev := rec.findByKind("oauth.linked"); ev == nil {
		t.Errorf("expected oauth.linked; got %v", rec.kinds())
	} else if ev.Meta["provider"] != "stub" {
		t.Errorf("provider = %q", ev.Meta["provider"])
	}

	// Second callback, same provider+id → existing link → oauth.login.
	w2 := runCallback(t, mgr, r, "stub")
	if w2.Code != http.StatusFound {
		t.Fatalf("second callback: %d", w2.Code)
	}
	if ev := rec.findByKind("oauth.login"); ev == nil {
		t.Errorf("expected oauth.login; got %v", rec.kinds())
	}

	// Collision: pre-existing email account, different provider id → refused.
	store2 := newLinkingUserStore()
	store2.preExistingUser("victim@example.com")
	rec2 := &recordingSink{}
	mgr2 := New(AuthConfig{
		JWTSecret: "test-secret", SessionTTL: time.Hour,
		SessionCookie: "session_id", UserStore: store2, DevMode: true, AuditSink: rec2,
	})
	mgr2.Use(NewOAuth2Plugin(OAuth2Config{
		Providers: map[string]OAuth2Provider{"stub": &stubOAuthProvider{
			name: "stub",
			userInfo: &OAuth2UserInfo{
				ID:            "attacker-id",
				Email:         "victim@example.com",
				Provider:      "stub",
				EmailVerified: true,
			},
		}},
		StateSecret: "test-secret",
	}))
	if err := mgr2.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r2 := router.New()
	mgr2.RegisterRoutes(r2)
	w3 := runCallback(t, mgr2, r2, "stub")
	if w3.Code != http.StatusConflict {
		t.Fatalf("collision callback: %d, want 409", w3.Code)
	}
	if ev := rec2.findByKind("oauth.refused"); ev == nil {
		t.Errorf("expected oauth.refused; got %v", rec2.kinds())
	} else if ev.Meta["reason"] != "link_conflict" {
		t.Errorf("reason = %q", ev.Meta["reason"])
	}
}
