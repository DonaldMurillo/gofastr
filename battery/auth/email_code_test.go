package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

type codeSender struct {
	email, code string
	sent        int
}

func (s *codeSender) SendSignInCode(_ context.Context, email, code string) error {
	s.email, s.code = email, code
	s.sent++
	return nil
}

type emailCodeFixture struct {
	mgr    *AuthManager
	users  *memoryUserStore
	plugin *EmailCodePlugin
	sender *codeSender
	twofa  *TwoFAPlugin
	r      *router.Router
}

type emailCodeOpts struct {
	store  MagicLinkTokenStore
	secret string
	twofa  bool
	mail   *RateLimiterConfig
}

func newEmailCodeFixture(t *testing.T, o emailCodeOpts) *emailCodeFixture {
	t.Helper()
	if o.secret == "" {
		o.secret = "test-secret-for-email-codes"
	}
	if o.store == nil {
		o.store = NewMemoryMagicLinkTokenStore()
	}
	if o.mail == nil {
		o.mail = &RateLimiterConfig{MaxAttempts: 1000, Window: time.Minute, BlockDuration: time.Minute}
	}
	users := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:            o.secret,
		SessionTTL:           time.Hour,
		SessionCookie:        "session_id",
		UserStore:            users,
		AllowInMemoryStores:  true,
		MailAddressRateLimit: o.mail,
	})
	f := &emailCodeFixture{mgr: mgr, users: users, sender: &codeSender{}}
	cfg := EmailCodeConfig{
		EmailSender:  f.sender,
		TokenStore:   o.store,
		OnSuccessURL: "/app",
	}
	if o.twofa {
		f.twofa = NewTwoFAPlugin(TwoFAConfig{})
		mgr.Use(f.twofa)
		cfg.TwoFactorURL = "/login/2fa"
	}
	f.plugin = NewEmailCodePlugin(cfg)
	mgr.Use(f.plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	f.r = router.New()
	mgr.RegisterRoutes(f.r)
	return f
}

func (f *emailCodeFixture) post(t *testing.T, path string, body map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	return w
}

// send asks for a code and returns the challenge cookie and the code mailed.
func (f *emailCodeFixture) send(t *testing.T, email string) (*http.Cookie, string) {
	t.Helper()
	w := f.post(t, "/auth/email-code/send", map[string]string{"email": email})
	if w.Code != http.StatusOK {
		t.Fatalf("send: %d %s", w.Code, w.Body)
	}
	c := cookieNamed(w.Result().Cookies(), f.plugin.cookieName())
	if c == nil || c.Value == "" {
		t.Fatal("send set no challenge cookie")
	}
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("challenge cookie must be HttpOnly and SameSite=Strict: %+v", c)
	}
	return c, f.sender.code
}

func (f *emailCodeFixture) verify(t *testing.T, code string, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	if c == nil {
		return f.post(t, "/auth/email-code/verify", map[string]string{"code": code})
	}
	return f.post(t, "/auth/email-code/verify", map[string]string{"code": code}, c)
}

// wrong returns a code that is not want.
func wrong(want string) string {
	if want == "00000000" {
		return "11111111"
	}
	return "00000000"
}

func TestEmailCode_SendThenVerify_SignsInNewVerifiedAccount(t *testing.T) {
	f := newEmailCodeFixture(t, emailCodeOpts{})
	c, code := f.send(t, "New@Example.com")
	if f.sender.email != "new@example.com" {
		t.Fatalf("code mailed to %q, want the canonical address", f.sender.email)
	}
	if !regexp.MustCompile(`^\d{8}$`).MatchString(code) {
		t.Fatalf("code %q is not eight digits", code)
	}

	// Typed with a space in the middle, as people do.
	w := f.verify(t, code[:4]+" "+code[4:], c)
	if w.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", w.Code, w.Body)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["redirect"] != "/app" || resp["two_factor_required"] != nil {
		t.Fatalf("response: %v", resp)
	}
	sc := cookieNamed(w.Result().Cookies(), "session_id")
	if sc == nil {
		t.Fatal("no session cookie")
	}
	sess, err := f.mgr.SessionStore().Get(context.Background(), sc.Value)
	if err != nil || sess.PendingTwoFactor {
		t.Fatalf("session: %+v %v", sess, err)
	}
	u, _, err := f.users.FindByEmail(context.Background(), "new@example.com")
	if err != nil || u.GetID() != sess.UserID {
		t.Fatalf("account: %v %v", u, err)
	}
	if ok, _ := f.users.IsEmailVerified(context.Background(), u.GetID()); !ok {
		t.Fatal("the code proved the mailbox, so the new account is verified")
	}
	if cl := cookieNamed(w.Result().Cookies(), f.plugin.cookieName()); cl == nil || cl.MaxAge >= 0 {
		t.Fatal("verify must clear the challenge cookie")
	}

	// Spent: the same code and cookie sign in nobody a second time.
	if w := f.verify(t, code, c); w.Code != http.StatusUnauthorized {
		t.Fatalf("replay: %d", w.Code)
	}
}

// The code alone is not a credential: it works only next to the challenge
// cookie of the browser that asked for it.
func TestEmailCode_CodeIsBoundToTheRequestingBrowser(t *testing.T) {
	f := newEmailCodeFixture(t, emailCodeOpts{})
	_, victimCode := f.send(t, "victim@example.com")
	attackerCookie, _ := f.send(t, "attacker@example.com")

	if w := f.verify(t, victimCode, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("no cookie: %d", w.Code)
	}
	if w := f.verify(t, victimCode, attackerCookie); w.Code != http.StatusUnauthorized {
		t.Fatalf("another challenge's cookie: %d", w.Code)
	}
	forged := &http.Cookie{Name: f.plugin.cookieName(), Value: strings.Repeat("ab", 32)}
	if w := f.verify(t, victimCode, forged); w.Code != http.StatusUnauthorized {
		t.Fatalf("made-up cookie: %d", w.Code)
	}
}

func TestEmailCode_ChallengeDroppedAfterFiveGuesses(t *testing.T) {
	f := newEmailCodeFixture(t, emailCodeOpts{})
	c, code := f.send(t, "alice@example.com")
	for i := 0; i < emailCodeChallengeGuesses-1; i++ {
		if w := f.verify(t, wrong(code), c); w.Code != http.StatusUnauthorized {
			t.Fatalf("guess %d: %d", i, w.Code)
		}
	}
	// The fifth guess is still allowed, and the right code works.
	if w := f.verify(t, code, c); w.Code != http.StatusOK {
		t.Fatalf("right code on the last guess: %d %s", w.Code, w.Body)
	}

	c, code = f.send(t, "alice@example.com")
	for i := 0; i < emailCodeChallengeGuesses; i++ {
		f.verify(t, wrong(code), c)
	}
	w := f.verify(t, code, c)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth guess: %d %s", w.Code, w.Body)
	}
	if cookieNamed(w.Result().Cookies(), "session_id") != nil {
		t.Fatal("a spent challenge must sign in nobody")
	}
	if _, err := f.plugin.peeker.PeekToken(context.Background(), c.Value); err == nil {
		t.Fatal("a spent challenge is dropped from the store")
	}
}

// New challenges do not reset the guess budget: it is per address too.
func TestEmailCode_AddressGuessBudgetSpansChallenges(t *testing.T) {
	f := newEmailCodeFixture(t, emailCodeOpts{})
	budget := f.plugin.config.AddressGuessLimit.MaxAttempts
	spent := 0
	for spent < budget {
		c, code := f.send(t, "bob@example.com")
		for i := 0; i < emailCodeChallengeGuesses-1 && spent < budget; i++ {
			f.verify(t, wrong(code), c)
			spent++
		}
	}
	c, code := f.send(t, "bob@example.com")
	if w := f.verify(t, code, c); w.Code != http.StatusTooManyRequests {
		t.Fatalf("past the address budget: %d %s", w.Code, w.Body)
	}
	// Another address is untouched.
	c, code = f.send(t, "carol@example.com")
	if w := f.verify(t, code, c); w.Code != http.StatusOK {
		t.Fatalf("other address: %d", w.Code)
	}
}

// In the per-address mail cooldown, send answers exactly as it does when
// it mails, cookie included, and the decoy cookie signs in nobody.
func TestEmailCode_AddressCooldownLooksLikeASend(t *testing.T) {
	f := newEmailCodeFixture(t, emailCodeOpts{mail: &RateLimiterConfig{MaxAttempts: 1, Window: time.Hour, BlockDuration: time.Hour}})
	f.send(t, "dana@example.com")
	firstCode := f.sender.code
	decoy, _ := f.send(t, "dana@example.com")
	if f.sender.sent != 1 {
		t.Fatalf("the cooldown must hold back the mail: sent %d", f.sender.sent)
	}
	if w := f.verify(t, firstCode, decoy); w.Code != http.StatusUnauthorized {
		t.Fatalf("decoy cookie: %d", w.Code)
	}
}

// Nothing a client can try tells a cooldown challenge from a mailed one:
// replaying either cookie on a second send gets the same answer, and
// both lock after the same number of wrong guesses.
func TestEmailCode_CooldownChallengeIsIndistinguishable(t *testing.T) {
	cooldown := &RateLimiterConfig{MaxAttempts: 1, Window: time.Hour, BlockDuration: time.Hour}
	resend := func(f *emailCodeFixture, c *http.Cookie) (int, bool, string) {
		w := f.post(t, "/auth/email-code/send", map[string]string{"email": "hal@example.com"}, c)
		return w.Code, cookieNamed(w.Result().Cookies(), f.plugin.cookieName()) != nil, w.Body.String()
	}

	mailed := newEmailCodeFixture(t, emailCodeOpts{mail: cooldown})
	real, _ := mailed.send(t, "hal@example.com")

	held := newEmailCodeFixture(t, emailCodeOpts{mail: cooldown})
	held.send(t, "hal@example.com") // another browser spends the cooldown
	decoy, _ := held.send(t, "hal@example.com")
	if held.sender.sent != 1 {
		t.Fatalf("the cooldown challenge's code must be mailed to no one: sent %d", held.sender.sent)
	}

	aCode, aCookie, aBody := resend(mailed, real)
	bCode, bCookie, bBody := resend(held, decoy)
	if aCode != bCode || aCookie != bCookie || aBody != bBody {
		t.Fatalf("resend answers differ: mailed (%d, cookie %v, %q) vs cooldown (%d, cookie %v, %q)",
			aCode, aCookie, aBody, bCode, bCookie, bBody)
	}

	for name, c := range map[string]*http.Cookie{"mailed": real, "cooldown": decoy} {
		f := mailed
		if name == "cooldown" {
			f = held
		}
		for i := 0; i < emailCodeChallengeGuesses; i++ {
			f.verify(t, "00000000", c)
		}
		if w := f.verify(t, "00000000", c); w.Code != http.StatusTooManyRequests {
			t.Fatalf("%s challenge past its guesses: %d", name, w.Code)
		}
	}
}

// A resend inside the cooldown from the browser holding the live
// challenge keeps it: the code already mailed still signs in.
func TestEmailCode_ResendInCooldownKeepsLiveChallenge(t *testing.T) {
	f := newEmailCodeFixture(t, emailCodeOpts{mail: &RateLimiterConfig{MaxAttempts: 1, Window: time.Hour, BlockDuration: time.Hour}})
	c, code := f.send(t, "ed@example.com")
	w := f.post(t, "/auth/email-code/send", map[string]string{"email": "ed@example.com"}, c)
	if w.Code != http.StatusOK || f.sender.sent != 1 {
		t.Fatalf("resend: %d, sent %d", w.Code, f.sender.sent)
	}
	if got := cookieNamed(w.Result().Cookies(), f.plugin.cookieName()); got != nil {
		t.Fatal("the resend must not replace the live challenge cookie")
	}
	if w := f.verify(t, code, c); w.Code != http.StatusOK {
		t.Fatalf("the first code after a resend: %d %s", w.Code, w.Body)
	}
}

func TestEmailCode_ResendInCooldownForAnotherAddressGetsDecoy(t *testing.T) {
	f := newEmailCodeFixture(t, emailCodeOpts{mail: &RateLimiterConfig{MaxAttempts: 1, Window: time.Hour, BlockDuration: time.Hour}})
	c, _ := f.send(t, "fran@example.com")
	f.send(t, "gus@example.com") // spends gus's cooldown from another browser
	w := f.post(t, "/auth/email-code/send", map[string]string{"email": "gus@example.com"}, c)
	got := cookieNamed(w.Result().Cookies(), f.plugin.cookieName())
	if got == nil || got.Value == c.Value {
		t.Fatal("a cooldown send for another address must answer with a decoy cookie")
	}
}

func TestEmailCode_CrossSiteFormsRefused(t *testing.T) {
	f := newEmailCodeFixture(t, emailCodeOpts{})
	c, code := f.send(t, "erin@example.com")
	for _, tc := range []struct{ path, field, value string }{
		{"/auth/email-code/send", "email", "erin@example.com"},
		{"/auth/email-code/verify", "code", code},
	} {
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(url.Values{tc.field: {tc.value}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.AddCookie(c)
		w := httptest.NewRecorder()
		f.r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s cross-site: %d", tc.path, w.Code)
		}
	}
	// The refused verify spent nothing: the code still works.
	if w := f.verify(t, code, c); w.Code != http.StatusOK {
		t.Fatalf("after the refused forms: %d", w.Code)
	}
}

func TestEmailCode_FormFlowRedirects(t *testing.T) {
	f := newEmailCodeFixture(t, emailCodeOpts{})
	form := func(path string, v url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(v.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		for _, c := range cookies {
			req.AddCookie(c)
		}
		w := httptest.NewRecorder()
		f.r.ServeHTTP(w, req)
		return w
	}
	w := form("/auth/email-code/send", url.Values{"email": {"fay@example.com"}, "next": {"/login/code"}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login/code" {
		t.Fatalf("send: %d %q", w.Code, w.Header().Get("Location"))
	}
	c := cookieNamed(w.Result().Cookies(), f.plugin.cookieName())
	w = form("/auth/email-code/verify", url.Values{"code": {f.sender.code}}, c)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/app" {
		t.Fatalf("verify: %d %q", w.Code, w.Header().Get("Location"))
	}
	if cookieNamed(w.Result().Cookies(), "session_id") == nil {
		t.Fatal("no session cookie")
	}
}

func TestEmailCode_TwoFactorAccountStaysPending(t *testing.T) {
	f := newEmailCodeFixture(t, emailCodeOpts{twofa: true})
	u := seedUser(t, f.users, "gail@example.com", "pw-irrelevant-1")
	_ = f.users.MarkEmailVerified(context.Background(), u.GetID())
	if err := f.twofa.store.SetTwoFA(context.Background(), u.GetID(), &TwoFAState{Enabled: true, Verified: true, Secret: GenerateSecret()}); err != nil {
		t.Fatal(err)
	}
	c, code := f.send(t, "gail@example.com")
	w := f.verify(t, code, c)
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if w.Code != http.StatusOK || resp["two_factor_required"] != true || resp["redirect"] != "/login/2fa" {
		t.Fatalf("verify: %d %v", w.Code, resp)
	}
	sess, _ := f.mgr.SessionStore().Get(context.Background(), cookieNamed(w.Result().Cookies(), "session_id").Value)
	if !sess.PendingTwoFactor {
		t.Fatal("a code alone must not pass the second factor")
	}
}

func TestEmailCode_InitNeedsTwoFactorURLNextToTwoFA(t *testing.T) {
	mgr := New(AuthConfig{JWTSecret: "s", UserStore: newMemoryUserStore(), AllowInMemoryStores: true})
	mgr.Use(NewTwoFAPlugin(TwoFAConfig{}))
	mgr.Use(NewEmailCodePlugin(EmailCodeConfig{EmailSender: &codeSender{}}))
	if err := mgr.Init(nil); err == nil || !strings.Contains(err.Error(), "TwoFactorURL") {
		t.Fatalf("Init: %v", err)
	}
}

func TestEmailCode_NoSenderFailsClosed(t *testing.T) {
	mgr := New(AuthConfig{JWTSecret: "s", UserStore: newMemoryUserStore(), AllowInMemoryStores: true})
	mgr.Use(NewEmailCodePlugin(EmailCodeConfig{}))
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodPost, "/auth/email-code/send", strings.NewReader(`{"email":"a@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("send without a sender: %d", w.Code)
	}
}

// Two replicas sharing the token store and JWTSecret check each other's
// codes; a replica with another secret does not.
func TestEmailCode_ReplicasShareCodes(t *testing.T) {
	store := NewMemoryMagicLinkTokenStore()
	a := newEmailCodeFixture(t, emailCodeOpts{store: store})
	b := newEmailCodeFixture(t, emailCodeOpts{store: store})
	other := newEmailCodeFixture(t, emailCodeOpts{store: store, secret: "a-different-secret-entirely"})

	c, code := a.send(t, "hal@example.com")
	if w := other.verify(t, code, c); w.Code != http.StatusUnauthorized {
		t.Fatalf("another secret: %d", w.Code)
	}
	if w := b.verify(t, code, c); w.Code != http.StatusOK {
		t.Fatalf("replica b: %d %s", w.Code, w.Body)
	}
}

// A password reset or change drops outstanding codes, as it drops magic
// links: the code an attacker read from the mailbox stops working.
func TestEmailCode_PasswordResetDropsOutstandingCodes(t *testing.T) {
	f := newEmailCodeFixture(t, emailCodeOpts{})
	u := seedUser(t, f.users, "ivy@example.com", "pw-irrelevant-2")
	_ = f.users.MarkEmailVerified(context.Background(), u.GetID())
	c, code := f.send(t, "Ivy@Example.com")
	if err := f.mgr.dropMagicLinks(context.Background(), u.GetID(), "password_reset", ""); err != nil {
		t.Fatal(err)
	}
	if w := f.verify(t, code, c); w.Code != http.StatusUnauthorized {
		t.Fatalf("after the reset: %d", w.Code)
	}
}

// A magic-link token is not an email-code challenge, and the reverse.
func TestEmailCode_TokensDoNotCrossFlows(t *testing.T) {
	f := newEmailCodeFixture(t, emailCodeOpts{})
	link, _ := createPurposeToken(context.Background(), f.plugin.tokenStore, purposeMagicLink, "jo@example.com", time.Minute)
	if w := f.verify(t, f.plugin.codeFor(link), &http.Cookie{Name: f.plugin.cookieName(), Value: link}); w.Code != http.StatusUnauthorized {
		t.Fatalf("magic-link token as a challenge: %d", w.Code)
	}
	if _, err := f.plugin.peeker.PeekToken(context.Background(), link); err != nil {
		t.Fatal("the refused attempt must not burn the magic link")
	}
	// Refused before any guess is spent: past a challenge's budget it
	// still reads as an unknown challenge, never as a locked one.
	for i := 0; i < emailCodeChallengeGuesses; i++ {
		if w := f.verify(t, f.plugin.codeFor(link), &http.Cookie{Name: f.plugin.cookieName(), Value: link}); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d with a magic-link token: %d", i+2, w.Code)
		}
	}
}

// DevMode logs hashes of the address and code, never the code: the code
// is a credential, and anyone reading the log could sign in with it.
func TestEmailCode_DevModeLogsNoCode(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	mgr := New(AuthConfig{JWTSecret: "s", UserStore: newMemoryUserStore(), AllowInMemoryStores: true})
	p := NewEmailCodePlugin(EmailCodeConfig{DevMode: true})
	mgr.Use(p)
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodPost, "/auth/email-code/send", strings.NewReader(`{"email":"dev@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("send: %d", w.Code)
	}
	c := cookieNamed(w.Result().Cookies(), p.cookieName())
	code := p.codeFor(c.Value)
	if strings.Contains(buf.String(), code) || strings.Contains(buf.String(), "dev@example.com") {
		t.Fatalf("the dev log carries the code or the address: %s", buf.String())
	}
	if !strings.Contains(buf.String(), hashedIdentifier(code)) {
		t.Fatalf("the dev log should carry the code's hash: %s", buf.String())
	}
}

// failingLimitStore is a guess-budget store that is down.
type failingLimitStore struct{}

func (failingLimitStore) Allow(context.Context, string, RateLimiterConfig) (bool, time.Duration, error) {
	return false, time.Second, errors.New("limiter store unreachable")
}

// A guess-budget store error refuses the guess with 503 and leaves the
// challenge alone: an outage is neither a spent budget nor an unmetered one.
func TestEmailCode_GuessBudgetStoreErrorRefuses(t *testing.T) {
	f := newEmailCodeFixture(t, emailCodeOpts{})
	c, code := f.send(t, "eve@example.com")
	f.plugin.address = newScopedRateLimiter(RateLimiterConfig{MaxAttempts: 10, Window: time.Hour, BlockDuration: time.Hour, Store: failingLimitStore{}}, "emailcode_address")
	if w := f.verify(t, code, c); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("verify with the budget store down: %d, want 503", w.Code)
	}
	if _, err := f.plugin.peeker.PeekToken(context.Background(), c.Value); err != nil {
		t.Fatal("a store error must not drop the challenge")
	}
}

// noCheckerUsers is a user store that cannot say whether an address was
// proven.
type noCheckerUsers struct{ UserStore }

// A mailbox sign-in to an existing account on a store that cannot say
// whether the address was proven is refused, not guessed at: "verified"
// would sign the owner into a squatter's account, "unverified" would wipe
// a real owner's password on every sign-in.
func TestMailboxSignInRefusesStoreWithoutVerifiedChecker(t *testing.T) {
	users := newMemoryUserStore()
	seedUser(t, users, "ida@example.com", "pw-irrelevant-3")
	mgr := New(AuthConfig{JWTSecret: "s", SessionCookie: "session_id", UserStore: noCheckerUsers{users}, AllowInMemoryStores: true})
	sender := &codeSender{}
	p := NewEmailCodePlugin(EmailCodeConfig{EmailSender: sender})
	mgr.Use(p)
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	f := &emailCodeFixture{mgr: mgr, plugin: p, sender: sender, r: r}
	c, code := f.send(t, "ida@example.com")
	w := f.verify(t, code, c)
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), reasonStoreNoVerifiedChecker.Code()) {
		t.Fatalf("verify on a store without EmailVerifiedChecker: %d %s", w.Code, w.Body)
	}
	if cookieNamed(w.Result().Cookies(), "session_id") != nil {
		t.Fatal("no session for a sign-in the store cannot judge")
	}
}
