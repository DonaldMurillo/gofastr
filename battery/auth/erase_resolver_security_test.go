package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

// keepEmailResolver restores the process-wide email resolver after a test
// that re-registers it.
func keepEmailResolver(t *testing.T) {
	t.Helper()
	prev, had := datexport.ResolveIdentity(datexport.IdentityEmail)
	t.Cleanup(func() {
		if had {
			datexport.RegisterIdentityResolver(datexport.IdentityEmail, prev)
		} else {
			datexport.UnregisterIdentityResolver(datexport.IdentityEmail)
		}
	})
}

// wrappedUsers is a host's own user store around the battery's: not an
// *EntityUserStore, so the battery cannot know its table.
type wrappedUsers struct{ UserStore }

// A resolver the host registered for its own user table survives Init:
// re-registering the battery's default over it made erasure resolve
// nothing and skip every email-keyed token while reporting success.
func TestInitKeepsHostEmailResolver(t *testing.T) {
	keepEmailResolver(t)
	datexport.RegisterIdentityResolver(datexport.IdentityEmail, datexport.DataIdentityResolver{
		Table: "members", IDColumn: "member_id", ValueColumn: "mail",
	})
	mgr := New(AuthConfig{JWTSecret: "x", DevMode: true, UserStore: wrappedUsers{newMemoryUserStore()}, AllowInMemoryStores: true})
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.OnStop(context.Background()) })
	r, ok := datexport.ResolveIdentity(datexport.IdentityEmail)
	if !ok || r.Table != "members" || r.IDColumn != "member_id" || r.ValueColumn != "mail" {
		t.Fatalf("host resolver replaced: %+v", r)
	}
	if r.Canonicalize == nil || r.Canonicalize("Mixed@Example.com") != "mixed@example.com" {
		t.Fatal("the host resolver gains the battery's canonicalizer when it has none")
	}
}

// The resolved email is already canonical; the eraser must not run the
// canonicalizer over it again. A host canonicalizer that is not idempotent
// otherwise moved the address off the tokens it was written into.
func TestMemoryEraserCanonicalizesOnce(t *testing.T) {
	keepEmailResolver(t)
	ctx := context.Background()
	once := func(s string) (string, error) { return "c-" + strings.ToLower(s), nil }
	tokens := NewMemoryMagicLinkTokenStore()
	mgr := New(AuthConfig{JWTSecret: "x", DevMode: true, UserStore: newMemoryUserStore(), AllowInMemoryStores: true, CanonicalizeEmail: once})
	mgr.Use(NewEmailCodePlugin(EmailCodeConfig{TokenStore: tokens, DevMode: true}))
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.OnStop(context.Background()) })
	canonical, _ := once("Ann@Example.com")
	if _, err := createPurposeToken(ctx, tokens, purposeEmailCode, canonical, time.Hour); err != nil {
		t.Fatal(err)
	}
	eraser := memoryEraser(t, mgr)
	n, err := eraser.EraseResolved(ctx, datexport.ErasedUser{ID: "u1", Identities: map[datexport.IdentityKind]string{datexport.IdentityEmail: canonical}}, false)
	if err != nil || n != 1 {
		t.Fatalf("erased %d, %v; want the one code for %q", n, err, canonical)
	}
}

// failingUsers fails every lookup, as a store that is down does.
type failingUsers struct{ *memoryUserStore }

func (failingUsers) FindByID(context.Context, string) (User, error) {
	return nil, errors.New("user store unreachable")
}

// When no email resolved and the user store cannot say, the erasure is
// incomplete and says so; an absent user is not an error.
func TestMemoryEraserReportsUserLookupFailure(t *testing.T) {
	ctx := context.Background()
	for name, users := range map[string]UserStore{"failing": failingUsers{newMemoryUserStore()}, "absent": newMemoryUserStore()} {
		t.Run(name, func(t *testing.T) {
			keepEmailResolver(t)
			tokens := NewMemoryMagicLinkTokenStore()
			mgr := New(AuthConfig{JWTSecret: "x", DevMode: true, UserStore: users, AllowInMemoryStores: true})
			mgr.Use(NewEmailCodePlugin(EmailCodeConfig{TokenStore: tokens, DevMode: true}))
			if err := mgr.Init(nil); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = mgr.OnStop(context.Background()) })
			if _, err := createPurposeToken(ctx, tokens, purposeEmailCode, "bo@example.com", time.Hour); err != nil {
				t.Fatal(err)
			}
			_, err := memoryEraser(t, mgr).EraseResolved(ctx, datexport.ErasedUser{ID: "u-missing"}, false)
			if name == "failing" && err == nil {
				t.Fatal("a failed user lookup must fail the store erasure, not report it complete")
			}
			if name == "absent" && err != nil {
				t.Fatalf("an absent user is nothing left to match: %v", err)
			}
		})
	}
}

func memoryEraser(t *testing.T, mgr *AuthManager) datexport.StoreEraser {
	t.Helper()
	for _, e := range datexport.AllStoreErasers() {
		if e.Name == mgr.memoryEraseName {
			return e
		}
	}
	t.Fatal("no in-memory store eraser registered")
	return datexport.StoreEraser{}
}

// flakyPeek is a token store whose reads can fail.
type flakyPeek struct {
	*MemoryMagicLinkTokenStore
	fail bool
}

func (f *flakyPeek) PeekToken(ctx context.Context, token string) (string, error) {
	if f.fail {
		return "", errors.New("token store read timed out")
	}
	return f.MemoryMagicLinkTokenStore.PeekToken(ctx, token)
}

// A resend in the cooldown whose challenge lookup fails keeps the browser's
// cookie and asks it to retry: reading the failure as "no challenge" swapped
// a live, mailed challenge for one whose code nobody has.
func TestEmailCode_ResendKeepsChallengeOnStoreReadFailure(t *testing.T) {
	store := &flakyPeek{MemoryMagicLinkTokenStore: NewMemoryMagicLinkTokenStore()}
	f := newEmailCodeFixture(t, emailCodeOpts{store: store, mail: &RateLimiterConfig{MaxAttempts: 1, Window: time.Hour, BlockDuration: time.Hour}})
	c, code := f.send(t, "cy@example.com")
	store.fail = true
	w := f.post(t, "/auth/email-code/send", map[string]string{"email": "cy@example.com"}, c)
	store.fail = false
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("resend with a failing store read: %d, want 503", w.Code)
	}
	if cookieNamed(w.Result().Cookies(), f.plugin.cookieName()) != nil {
		t.Fatal("the failing resend must not replace the challenge cookie")
	}
	if w := f.verify(t, code, c); w.Code != http.StatusOK {
		t.Fatalf("the mailed code after the failed resend: %d %s", w.Code, w.Body)
	}
}
