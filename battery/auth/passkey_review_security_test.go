package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// A registration challenge completes only in the session that asked for
// it, which is the one that passed any step-up: another session of the
// same account, holding the challenge and its binding, is refused, and so
// is a body without the binding.
func TestPasskeyRegistrationIsBoundToItsSession(t *testing.T) {
	f := newPasskeyFixture(t)
	asker, other := f.session(t, "u-alice"), f.session(t, "u-alice")
	a := newFakeAuthenticator(t, coseAlgES256)
	ch := challengeOf(t, f.do(t, "POST", "/passkey/register/options", asker, nil))
	if w := f.do(t, "POST", "/passkey/register", other, registerBody(a, ch)); w.Code == http.StatusCreated {
		t.Fatal("another session of the account completed the asking session's registration")
	}
	ch = challengeOf(t, f.do(t, "POST", "/passkey/register/options", asker, nil))
	body := registerBody(a, ch)
	delete(body, "binding")
	if w := f.do(t, "POST", "/passkey/register", asker, body); w.Code == http.StatusCreated {
		t.Fatal("a registration without its session binding was accepted")
	}
	ch = challengeOf(t, f.do(t, "POST", "/passkey/register/options", asker, nil))
	if w := f.do(t, "POST", "/passkey/register", asker, registerBody(a, ch)); w.Code != http.StatusCreated {
		t.Fatalf("the asking session's own registration: %d %s", w.Code, w.Body)
	}
}

// A step-up names the account's passkeys in allowCredentials, so the
// authenticator may leave userHandle null: the step-up still verifies. A
// handle that is given must still be the passkey's account, and a
// discoverable sign-in still needs one.
func TestStepUpAcceptsAMissingUserHandle(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	s := f.session(t, "u-alice")
	f.age(t, a)
	step := func(handle string) *httptest.ResponseRecorder {
		ch := challengeOf(t, f.do(t, "POST", "/passkey/reauth/options", s, nil))
		body := loginBody(a, ch, "u-alice")
		body["userHandle"] = handle
		return f.do(t, "POST", "/passkey/reauth", s, body)
	}
	if w := step(""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "proof") {
		t.Fatalf("step-up without a user handle: %d %s", w.Code, w.Body)
	}
	if w := step(b64url.EncodeToString([]byte("u-bob"))); w.Code == http.StatusOK {
		t.Fatal("a step-up whose handle names another account was accepted")
	}
	ch := challengeOf(t, f.do(t, "POST", "/passkey/login/options", "", nil))
	body := loginBody(a, ch, "u-alice")
	body["userHandle"] = ""
	if w := f.do(t, "POST", "/passkey/login", "", body); w.Code != http.StatusBadRequest {
		t.Fatalf("a sign-in without a user handle: %d, want 400", w.Code)
	}
}

// noVerifiedStore is a user store that cannot say which addresses were
// proven.
type noVerifiedStore struct{ UserStore }

// The added-passkey notice goes only to a verified address, so a store
// that cannot say which are fails Init unless the notice is off.
func TestNoticeNeedsAVerifiedChecker(t *testing.T) {
	boot := func(pk PasskeyConfig) error {
		mgr := New(AuthConfig{JWTSecret: "s", UserStore: noVerifiedStore{newMemoryUserStore()}, AllowInMemoryStores: true})
		pk.RPID, pk.Origins = "example.com", []string{"https://example.com"}
		mgr.Use(NewPasskeyPlugin(pk))
		return mgr.Init(nil)
	}
	if err := boot(PasskeyConfig{NoticeSender: &stubEmailSender{}}); err == nil || !strings.Contains(err.Error(), "EmailVerifiedChecker") {
		t.Fatalf("notice with a store that cannot prove addresses: %v", err)
	}
	if err := boot(PasskeyConfig{NoAddedNotice: true}); err != nil {
		t.Fatalf("NoAddedNotice: %v", err)
	}
}

// scopeStore is a shared limiter store that records the scope of each
// call and counts per scope and key.
type scopeStore struct {
	mu     sync.Mutex
	scopes []string
	n      map[string]int
}

func (s *scopeStore) Allow(_ context.Context, key string, cfg RateLimiterConfig) (bool, time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.n == nil {
		s.n = map[string]int{}
	}
	s.scopes = append(s.scopes, cfg.Scope)
	s.n[cfg.Scope+"|"+key]++
	return s.n[cfg.Scope+"|"+key] <= cfg.MaxAttempts, time.Minute, nil
}

// Under a host's explicit Scope and one shared store, the list and
// autofill requests count under their own scopes, so they never spend
// the ceremonies' budget.
func TestPasskeyBackgroundLimitsKeepTheirOwnScope(t *testing.T) {
	store := &scopeStore{}
	p := NewPasskeyPlugin(PasskeyConfig{RateLimit: &RateLimiterConfig{MaxAttempts: 2, Window: time.Minute, BlockDuration: time.Minute, Scope: "pk", Store: store}})
	req := func() *http.Request { return httptest.NewRequest("GET", "https://example.com/auth/passkey/list", nil) }
	for range 3 {
		p.listLimit.guard(httptest.NewRecorder(), req())
		p.autofillLimit.guard(httptest.NewRecorder(), req())
	}
	if !p.limit.guard(httptest.NewRecorder(), req()) {
		t.Fatal("background requests spent the ceremonies' budget under a shared scope")
	}
	got := slices.Compact(slices.Sorted(slices.Values(store.scopes)))
	if !slices.Equal(got, []string{"pk", "pk-autofill", "pk-list"}) {
		t.Fatalf("scopes %v", got)
	}
}

// sharedSliceStore answers ListPasskeys with its own backing array.
type sharedSliceStore struct {
	PasskeyStore
	creds []PasskeyCredential
}

func (s *sharedSliceStore) ListPasskeys(context.Context, string) ([]PasskeyCredential, error) {
	return s.creds, nil
}

// The step-up options filter into a new slice: a store that hands out
// its own storage keeps every passkey.
func TestReauthOptionsLeaveTheStoresSliceAlone(t *testing.T) {
	f := newPasskeyFixture(t)
	f.enrol(t, "u-alice")
	creds, _ := f.pk.store.ListPasskeys(context.Background(), "u-alice")
	young := creds[0]
	young.ID, young.CreatedAt = []byte("young-passkey-id"), time.Now().Add(time.Hour)
	old := creds[0]
	old.CreatedAt = time.Now().Add(-48 * time.Hour)
	shared := &sharedSliceStore{PasskeyStore: f.pk.store, creds: []PasskeyCredential{young, old}}
	f.pk.store = shared
	f.do(t, "POST", "/passkey/reauth/options", f.session(t, "u-alice"), nil)
	if string(shared.creds[0].ID) != "young-passkey-id" || len(shared.creds[1].ID) == 0 {
		t.Fatalf("the store's slice was rewritten: %+v", shared.creds)
	}
}
