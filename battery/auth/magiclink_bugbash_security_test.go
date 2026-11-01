package auth

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Regression tests from the 2026-11-01 battery/auth bug bash: magic-link
// sign-in and the mail-dispatch endpoints.

// countingLinkSender counts magic-link sends per address.
type countingLinkSender struct {
	mu   sync.Mutex
	sent map[string]int
}

func (s *countingLinkSender) SendMagicLink(_ context.Context, email, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sent == nil {
		s.sent = map[string]int{}
	}
	s.sent[email]++
	return nil
}

func (s *countingLinkSender) count(email string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sent[email]
}

// Property: a magic-link plugin next to a 2FA plugin knows where a
// pending sign-in goes, as the passkey plugin does. The verify handler
// threw away MintSession's pending flag and always redirected to
// OnSuccessURL, which refuses a pending session: a 2FA user who
// followed their link landed on a 403 with no way to the challenge.
func TestMagicLinkInitRequiresTwoFactorURLWith2FA(t *testing.T) {
	for _, twofaFirst := range []bool{true, false} {
		mgr := New(AuthConfig{JWTSecret: "x", AllowInMemoryStores: true, UserStore: newMemoryUserStore()}) // not-a-secret: test fixture
		magic := NewMagicLinkPlugin(MagicLinkConfig{BaseURL: "http://localhost", DevMode: true})
		if twofaFirst {
			mgr.Use(NewTwoFAPlugin(TwoFAConfig{}))
			mgr.Use(magic)
		} else {
			mgr.Use(magic)
			mgr.Use(NewTwoFAPlugin(TwoFAConfig{}))
		}
		err := mgr.Init(nil)
		if err == nil || !strings.Contains(err.Error(), "TwoFactorURL") {
			t.Errorf("twofaFirst=%v: Init = %v, want a refusal naming MagicLinkConfig.TwoFactorURL", twofaFirst, err)
		}
	}
	mgr := New(AuthConfig{JWTSecret: "x", AllowInMemoryStores: true, UserStore: newMemoryUserStore()}) // not-a-secret: test fixture
	mgr.Use(NewMagicLinkPlugin(MagicLinkConfig{BaseURL: "http://localhost", DevMode: true, TwoFactorURL: "/login/2fa"}))
	mgr.Use(NewTwoFAPlugin(TwoFAConfig{}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init with TwoFactorURL: %v", err)
	}
}

func TestMagicLinkPendingSignInGoesToTwoFactorURL(t *testing.T) {
	_, _, magic, r := setupP17WithMagicLink(t)
	token, err := createPurposeToken(context.Background(), magic.tokenStore, purposeMagicLink, "alice@example.com", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, magicConfirmReq(token))
	if w.Code != http.StatusFound {
		t.Fatalf("verify = %d, want 302; body=%s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/login/2fa" {
		t.Errorf("pending magic-link sign-in redirected to %q, want the TwoFactorURL /login/2fa (OnSuccessURL refuses a pending session)", loc)
	}
}

// sendFrom posts a magic-link send for email from a distinct client IP,
// so the per-IP floor never trips.
func sendFrom(r http.Handler, email string, i int) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"email": email})
	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "198.51.100." + strconv.Itoa(i+1) + ":1234"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Property: one address gets a bounded number of links however many
// IPs ask. Send was limited per IP only, so a botnet (or one rotating
// proxy) could mail an unbounded stream of sign-in links to a victim's
// inbox. Past the per-address budget the answer stays the same uniform
// success, and nothing is minted or sent.
func TestMagicLinkSendPerAddressLimit(t *testing.T) {
	sender := &countingLinkSender{}
	mgr := New(AuthConfig{JWTSecret: "x", AllowInMemoryStores: true, UserStore: newMemoryUserStore()}) // not-a-secret: test fixture
	mgr.Use(NewMagicLinkPlugin(MagicLinkConfig{BaseURL: "http://localhost", EmailSender: sender}))
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)

	var first string
	for i := range 12 {
		w := sendFrom(r, "victim@example.com", i)
		if w.Code != http.StatusOK {
			t.Fatalf("send %d = %d, want the uniform 200; body=%s", i+1, w.Code, w.Body.String())
		}
		if i == 0 {
			first = w.Body.String()
		} else if w.Body.String() != first {
			t.Errorf("send %d body %q differs from the first %q: the throttle must not be observable", i+1, w.Body.String(), first)
		}
	}
	if got := sender.count("victim@example.com"); got != 1 {
		t.Errorf("SECURITY: [mail-flood] %d links mailed to one address from 12 IPs inside one cooldown, want 1", got)
	}
	// Another address has its own budget.
	sendFrom(r, "other@example.com", 50)
	if got := sender.count("other@example.com"); got != 1 {
		t.Errorf("another address got %d sends, want 1", got)
	}
}

func TestForgotPasswordPerAddressLimit(t *testing.T) {
	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{UserStore: store, DevMode: true})
	mgr.Use(NewCorePlugin().WithTwoFactorURL("/login/2fa"))
	sender := &stubEmailSender{}
	reset := NewPasswordResetPlugin(PasswordResetConfig{BaseURL: "http://localhost", EmailSender: sender})
	mgr.Use(reset)
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reset.OnStop(context.Background()) })
	seedUser(t, store.memoryUserStore, "victim@example.com", "hunter2222")
	r := mountRoutes(mgr)

	for i := range 12 {
		body, _ := json.Marshal(map[string]string{"email": "victim@example.com"})
		req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "198.51.100." + strconv.Itoa(i+1) + ":1234"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"sent":true}` {
			t.Fatalf("forgot %d = %d %q, want the uniform 200", i+1, w.Code, w.Body.String())
		}
	}
	// Delivery is queued; wait for it to drain.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sender.mu.Lock()
		n := sender.sendCount
		sender.mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	sender.mu.Lock()
	n := sender.sendCount
	sender.mu.Unlock()
	if n != 1 {
		t.Errorf("SECURITY: [mail-flood] %d reset mails sent to one address from 12 IPs inside one cooldown, want 1", n)
	}
}

// Property: the magiclink.requested event names the account when one
// exists, so an erasure, which scrubs the audit trail by actor id,
// removes it. Without the id the row kept the erased user's address.
func TestMagicLinkRequestedEventCarriesUserID(t *testing.T) {
	sink := &recordingSink{}
	users := newMemoryUserStore()
	mgr := New(AuthConfig{JWTSecret: "x", AllowInMemoryStores: true, UserStore: users, AuditSink: sink}) // not-a-secret: test fixture
	mgr.Use(NewMagicLinkPlugin(MagicLinkConfig{BaseURL: "http://localhost", EmailSender: &countingLinkSender{}}))
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	u := seedUser(t, users, "known@example.com", "hunter2222")
	r := router.New()
	mgr.RegisterRoutes(r)
	sendFrom(r, "known@example.com", 0)
	sendFrom(r, "nobody@example.com", 1)

	byEmail := map[string]string{}
	for _, ev := range sink.snapshot() {
		if ev.Kind == "magiclink.requested" {
			byEmail[ev.Email] = ev.UserID
		}
	}
	if got := byEmail["known@example.com"]; got != u.GetID() {
		t.Errorf("magiclink.requested for an existing account carries UserID %q, want %q: erasure scrubs audit rows by actor id", got, u.GetID())
	}
	if got, ok := byEmail["nobody@example.com"]; !ok || got != "" {
		t.Errorf("magiclink.requested for an unknown address: present=%v UserID=%q, want present with no id", ok, got)
	}
}

// Property: a token row that was written is returned, whatever the
// amortized sweep after it does. CreateToken inserted the row and then
// returned ("", err) when the sweep failed: the row was live but the
// caller got nothing (reset mailed no link, magic-link send answered
// 500), and lastSweep had already moved, so nothing retried for 15
// minutes.
func TestSQLMagicLinkCreateTokenSurvivesSweepFailure(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	s, err := NewSQLMagicLinkTokenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO magic_link_tokens (token, email, expires_at) VALUES ('stale', 'old@example.com', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER no_delete BEFORE DELETE ON magic_link_tokens BEGIN SELECT RAISE(ABORT, 'sweep refused'); END`); err != nil {
		t.Fatal(err)
	}
	tok, err := s.CreateToken(ctx, "new@example.com", time.Hour)
	if err != nil || tok == "" {
		t.Fatalf("CreateToken with a failing sweep = (%q, %v), want the live token and no error", tok, err)
	}
	if got, err := s.PeekToken(ctx, tok); err != nil || got != "new@example.com" {
		t.Fatalf("returned token does not resolve: %q, %v", got, err)
	}

	// The failed sweep did not count as a sweep, and does not retry on
	// every mint either (a lock-timeout DELETE would then run on each):
	// it retries after magicLinkSweepRetry, not the full interval.
	if _, err := db.Exec(`DROP TRIGGER no_delete`); err != nil {
		t.Fatal(err)
	}
	staleRows := func() int {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM magic_link_tokens WHERE token = 'stale'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if _, err := s.CreateToken(ctx, "next@example.com", time.Hour); err != nil {
		t.Fatal(err)
	}
	if staleRows() != 1 {
		t.Error("the mint right after a failed sweep swept again: a failing DELETE would run on every mint")
	}
	s.sweepMu.Lock()
	s.lastSweep = s.lastSweep.Add(-magicLinkSweepRetry)
	s.sweepMu.Unlock()
	if _, err := s.CreateToken(ctx, "later@example.com", time.Hour); err != nil {
		t.Fatal(err)
	}
	if staleRows() != 0 {
		t.Error("a failed sweep waited out the full interval instead of retrying after magicLinkSweepRetry")
	}
}

// The per-address limit is a cooldown, not an hourly block: anyone can
// spend an address's budget, so a long block lets a stranger stop the
// owner's mail. Mails a stranger triggers reach the owner, and the owner
// always holds a working link only while a link outlives the longest gap
// the limiter can force, Window + BlockDuration (2026-11-01 review,
// round 2: MaxAttempts 1 with an hour's block passed every other test).
func TestAddressLimitIsACooldownALinkOutlives(t *testing.T) {
	cfg := defaultAddressRateLimit()
	gap := cfg.Window + cfg.BlockDuration
	if gap > 5*time.Minute {
		t.Errorf("per-address limit can hold mail back %v, want a short cooldown", gap)
	}
	ml := NewMagicLinkPlugin(MagicLinkConfig{})
	pr := NewPasswordResetPlugin(PasswordResetConfig{})
	for name, ttl := range map[string]time.Duration{"magic link": ml.config.TokenTTL, "password reset": pr.cfg.TokenTTL} {
		if ttl <= gap {
			t.Errorf("%s TokenTTL %v does not outlive the per-address gap %v", name, ttl, gap)
		}
	}
}
