package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// The 2FA challenge was throttled per client address only. A password
// holder spreading guesses over many addresses (one IPv6 /64 is enough)
// guessed codes against one pending session without limit: the audit sent
// 200 wrong codes from 20 addresses, saw 200x401 and no 429, and the
// correct code still verified afterwards. Each session now gets a fixed
// number of code checks; the next attempt revokes it and the user has to
// sign in again, whatever address the guesses come from.
func TestChallengeGuessesCappedPerSession(t *testing.T) {
	ctx := context.Background()
	store := newMemoryUserStore()
	hash, err := HashPassword("guess-target-pw-1")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := store.CreateUser(ctx, "guess@example.com", hash, []string{"user"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mgr := New(AuthConfig{
		JWTSecret:           "cap-secret-0123456789abcdef-0123456789abcdef",
		SessionCookie:       "session_id",
		SessionTTL:          7 * 24 * time.Hour,
		UserStore:           store,
		AllowInMemoryStores: true,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewTwoFAPlugin(TwoFAConfig{})) // default per-address throttle
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)

	post := func(path, body, cookie, remote string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			req.Header.Set("Cookie", "session_id="+cookie)
		}
		if remote != "" {
			req.RemoteAddr = remote
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	login := func() string {
		w := post("/auth/login", `{"email":"guess@example.com","password":"guess-target-pw-1"}`, "", "")
		if w.Code != http.StatusOK {
			t.Fatalf("login: %d %s", w.Code, w.Body.String())
		}
		for _, c := range w.Result().Cookies() {
			if c.Name == "session_id" {
				return c.Value
			}
		}
		t.Fatal("no session cookie")
		return ""
	}

	S0 := login()
	w := post("/auth/2fa/enroll", "", S0, "")
	if w.Code != http.StatusOK {
		t.Fatalf("enroll: %d %s", w.Code, w.Body.String())
	}
	var er struct {
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &er)
	w = post("/auth/2fa/verify", fmt.Sprintf(`{"code":%q}`, totpNow(er.Secret)), S0, "")
	if w.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", w.Code, w.Body.String())
	}
	var vr struct {
		BackupCodes []string `json:"backup_codes"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &vr)

	P := login()
	valid := map[string]bool{}
	step := uint64(time.Now().Unix() / 30)
	for _, s := range []uint64{step - 1, step, step + 1} {
		valid[GenerateTOTP(er.Secret, s)] = true
	}
	wrong := func(i int) string {
		for {
			c := fmt.Sprintf("%06d", i%1000000)
			i++
			if !valid[c] {
				return c
			}
		}
	}

	// 200 wrong codes from 20 addresses in one /64, 10 each.
	checked := 0
	for h := 1; h <= 20; h++ {
		remote := fmt.Sprintf("[2001:db8:1:2::%x]:1111", h)
		for j := range 10 {
			w := post("/auth/2fa/challenge", fmt.Sprintf(`{"code":%q}`, wrong(h*1000+j)), P, remote)
			if strings.Contains(w.Body.String(), "invalid code") {
				checked++
			}
			if checked > 10 {
				t.Fatalf("%d wrong codes were checked against one pending session; want at most 10", checked)
			}
		}
	}
	if _, err := mgr.SessionStore().Get(ctx, P); err == nil {
		t.Fatal("pending session survived the guessing run")
	}
	// The correct code no longer completes the revoked session.
	w = post("/auth/2fa/challenge", fmt.Sprintf(`{"code":%q}`, vr.BackupCodes[0]), P, "[2001:db8:1:2:ffff::1]:1111")
	if w.Code == http.StatusOK {
		t.Fatalf("revoked session stepped up: %d %s", w.Code, w.Body.String())
	}

	// The account is not locked: a fresh login steps up normally.
	P2 := login()
	w = post("/auth/2fa/challenge", fmt.Sprintf(`{"code":%q}`, vr.BackupCodes[1]), P2, "198.51.100.77:1")
	if w.Code != http.StatusOK {
		t.Fatalf("fresh session refused: %d %s", w.Code, w.Body.String())
	}
}
