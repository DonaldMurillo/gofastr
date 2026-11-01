package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// listHooked runs a hook once, right after the claim's ListAccounts has
// read the account's links: a link written from inside the hook is one the
// claim's unlink loop never sees.
type listHooked struct {
	*EntityUserStore
	mu   sync.Mutex
	hook func()
}

func (h *listHooked) ListAccounts(ctx context.Context, userID string) ([]Account, error) {
	accts, err := h.EntityUserStore.ListAccounts(ctx, userID)
	h.mu.Lock()
	run := h.hook
	h.hook = nil
	h.mu.Unlock()
	if run != nil {
		run()
	}
	return accts, err
}

// squatterLink registers victim@example.com with a password (unverified),
// signs in, and starts linking provider. finish completes the link
// callback under that session and returns its status.
func squatterLink(t *testing.T, c *claimRig, provider string) (uid string, finish func() int) {
	t.Helper()
	const email, pw = "victim@example.com", "attacker-chosen-pw-1"
	if w := c.json("/auth/register", map[string]string{"email": email, "password": pw}, ""); w.Code >= 300 {
		t.Fatalf("setup: register = %d %s", w.Code, w.Body)
	}
	cookie := c.login(email, pw)
	if cookie == "" {
		t.Fatal("setup: squatter login refused")
	}
	uid = c.sessionUser(cookie)
	w1 := c.do(http.MethodGet, "/auth/oauth/"+provider+"/link", "", "", cookie)
	if w1.Code != http.StatusFound {
		t.Fatalf("setup: link redirect = %d %s", w1.Code, w1.Body)
	}
	loc := w1.Header().Get("Location")
	state := loc[strings.LastIndex(loc, "state=")+len("state="):]
	return uid, func() int {
		req := httptest.NewRequest(http.MethodGet, "/auth/oauth/"+provider+"/callback?state="+url.QueryEscape(state)+"&code=c", nil)
		for _, ck := range w1.Result().Cookies() {
			req.AddCookie(ck)
		}
		req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
		w := httptest.NewRecorder()
		c.r.ServeHTTP(w, req)
		return w.Code
	}
}

// A squatter's authenticated link callback that the owner's claim overtook
// between the link lookup and the upsert stored the link after the claim
// had unlinked everything, and the squatter's IdP identity signed back into
// the now-verified account (2026-10-31 O13). The callback re-checks its
// session and credential after the upsert and takes the link back.
func TestLinkCallbackOvertakenByClaim(t *testing.T) {
	var hooked *hookedLinks
	c := newClaimRig(t, func(s *EntityUserStore) UserStore {
		hooked = &hookedLinks{EntityUserStore: s}
		return hooked
	})
	uid, finish := squatterLink(t, c, "corp")
	hooked.arm(1, func() {
		if got := c.magicLink("victim@example.com"); got != uid {
			t.Errorf("setup: magic link signed into %s, want %s", got, uid)
		}
	})
	code := finish()
	hooked.mustHaveFired(t)
	if code == http.StatusFound {
		t.Error("SECURITY: a link callback overtaken by a claim answered 302")
	}
	if c.linked(uid, "corp") {
		t.Error("SECURITY: the squatter's link survived the claim")
	}
	if got, _ := c.oauth("corp"); got == uid {
		t.Error("SECURITY: the squatter's IdP identity signs into the claimed account")
	}
}

// The claim lists the links, unlinks them, and only then purged sessions:
// a link stored after the listing, by a callback whose session re-check
// ran before the purge, survived. The claim now purges sessions before it
// lists the links too.
func TestLinkLandsMidClaim(t *testing.T) {
	var hooked *listHooked
	c := newClaimRig(t, func(s *EntityUserStore) UserStore {
		hooked = &listHooked{EntityUserStore: s}
		return hooked
	})
	uid, finish := squatterLink(t, c, "corp")
	code := 0
	hooked.hook = func() { code = finish() }
	if got := c.magicLink("victim@example.com"); got != uid {
		t.Fatalf("setup: magic link signed into %s, want %s", got, uid)
	}
	if code == 0 {
		t.Fatal("setup: the claim never listed the account's links")
	}
	if code == http.StatusFound {
		t.Error("SECURITY: a link that landed mid-claim answered 302")
	}
	if c.linked(uid, "corp") {
		t.Error("SECURITY: a link that landed mid-claim survived it")
	}
}

// An authenticated link with nothing racing it still lands.
func TestLinkCallbackStillLinks(t *testing.T) {
	c := newClaimRig(t)
	uid, finish := squatterLink(t, c, "corp")
	if code := finish(); code != http.StatusFound {
		t.Fatalf("link callback = %d, want 302", code)
	}
	if !c.linked(uid, "corp") {
		t.Fatal("the link did not land")
	}
}
