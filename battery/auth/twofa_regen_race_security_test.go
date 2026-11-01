package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// regenRaceStore runs a one-shot hook inside the backup-code regeneration's
// read-modify-write: onGet right after the getAt-th GetTwoFA has read its
// row, onSwap right before a CompareAndSwapTwoFA writes. Whatever the hook
// does lands between the handler's read and its write.
type regenRaceStore struct {
	TwoFAStore
	mu            sync.Mutex
	onGet, onSwap func()
	getAt, gets   int
}

// The step-up guard reads the enrollment first; the handler's own read is
// the second.
const handlerRead = 2

func (s *regenRaceStore) take(h *func()) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := *h
	*h = nil
	return run
}

func (s *regenRaceStore) GetTwoFA(ctx context.Context, userID string) (*TwoFAState, error) {
	st, err := s.TwoFAStore.GetTwoFA(ctx, userID)
	s.mu.Lock()
	s.gets++
	fire := s.gets == s.getAt
	s.mu.Unlock()
	if fire {
		if run := s.take(&s.onGet); run != nil {
			run()
		}
	}
	return st, err
}

func (s *regenRaceStore) CompareAndSwapTwoFA(ctx context.Context, userID string, expect, next *TwoFAState) (bool, error) {
	if run := s.take(&s.onSwap); run != nil {
		run()
	}
	return s.TwoFAStore.(TwoFAStateSwapper).CompareAndSwapTwoFA(ctx, userID, expect, next)
}

func (s *regenRaceStore) EnsureSchema(ctx context.Context) error {
	if se, ok := s.TwoFAStore.(interface{ EnsureSchema(context.Context) error }); ok {
		return se.EnsureSchema(ctx)
	}
	return nil
}

func (s *regenRaceStore) mustHaveFired(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.onGet != nil || s.onSwap != nil {
		t.Fatal("the staged interleaving never ran; the test proves nothing")
	}
}

type regenRig struct {
	t      *testing.T
	r      *router.Router
	inner  TwoFAStore
	store  *regenRaceStore
	uid    string
	secret string
	main   string // stepped-up session
}

func newRegenRig(t *testing.T, inner TwoFAStore) *regenRig {
	t.Helper()
	store := &regenRaceStore{TwoFAStore: inner}
	users := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		AllowInMemoryStores: true,
		SessionCookie:       "session_id",
		SessionTTL:          time.Hour,
		UserStore:           users,
	})
	mgr.Use(NewCorePlugin().WithTwoFactorURL("/login/2fa"))
	mgr.Use(NewTwoFAPlugin(TwoFAConfig{Store: store}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	seedUser(t, users, "alice@test.com", "testpass")
	g := &regenRig{t: t, r: mountRoutes(mgr), inner: inner, store: store}
	g.main = loginSessionCookie(t, g.r)
	sess, err := mgr.SessionStore().Get(context.Background(), g.main)
	if err != nil {
		t.Fatal(err)
	}
	g.uid = sess.UserID
	var enroll struct {
		Secret string `json:"secret"`
	}
	w := g.post("/auth/2fa/enroll", "", g.main)
	if err := json.Unmarshal(w.Body.Bytes(), &enroll); err != nil || enroll.Secret == "" {
		t.Fatalf("enroll: %d %s", w.Code, w.Body.String())
	}
	g.secret = enroll.Secret
	if w := g.post("/auth/2fa/verify", fmt.Sprintf(`{"code":%q}`, g.code()), g.main); w.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", w.Code, w.Body.String())
	}
	return g
}

func (g *regenRig) code() string {
	return GenerateTOTP(g.secret, uint64(time.Now().Unix())/30)
}

func (g *regenRig) post(path, body, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w := httptest.NewRecorder()
	g.r.ServeHTTP(w, req)
	return w
}

func (g *regenRig) challenge(cookie, code string) int {
	return g.post("/auth/2fa/challenge", fmt.Sprintf(`{"code":%q}`, code), cookie).Code
}

func (g *regenRig) state() *TwoFAState {
	g.t.Helper()
	st, err := g.inner.GetTwoFA(context.Background(), g.uid)
	if err != nil {
		g.t.Fatal(err)
	}
	return st
}

// Regenerating backup codes read the enrollment, spent bcrypt time on new
// codes and wrote the whole stale row back. A challenge that consumed a
// TOTP step in that gap was undone, so the same code stepped up a second
// pending session; a disable in that gap was resurrected on the SQL store,
// which had no conditional write at all.
func TestBackupRegenKeepsConcurrentState(t *testing.T) {
	stores := map[string]func(t *testing.T) TwoFAStore{
		"memory": func(*testing.T) TwoFAStore { return NewMemoryTwoFAStore() },
		"sql": func(t *testing.T) TwoFAStore {
			db, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			t.Cleanup(func() { db.Close() })
			s, err := NewEntityTwoFAStore(db, "auth_twofa", testTwoFAStoreConfig())
			if err != nil {
				t.Fatal(err)
			}
			return s
		},
		"postgres": func(t *testing.T) TwoFAStore {
			s, err := NewEntityTwoFAStore(openPGForBattery(t), "auth_twofa", testTwoFAStoreConfig())
			if err != nil {
				t.Fatal(err)
			}
			return s
		},
	}
	for name, mk := range stores {
		for _, at := range []string{"after the read", "before the write"} {
			t.Run(name+"/challenge "+at, func(t *testing.T) {
				g := newRegenRig(t, mk(t))
				first := loginSessionCookie(t, g.r)
				second := loginSessionCookie(t, g.r)
				code := g.code()
				hook := func() {
					if got := g.challenge(first, code); got != http.StatusOK {
						t.Errorf("first challenge = %d, want 200", got)
					}
				}
				if at == "after the read" {
					g.store.onGet, g.store.gets, g.store.getAt = hook, 0, handlerRead
				} else {
					g.store.onSwap = hook
				}
				w := g.post("/auth/2fa/backup-codes", "", g.main)
				g.store.mustHaveFired(t)
				if w.Code != http.StatusOK {
					t.Fatalf("regenerate = %d %s, want 200", w.Code, w.Body.String())
				}
				if st := g.state(); st == nil || st.LastUsedStep == 0 {
					t.Fatal("regeneration rolled back the consumed TOTP step")
				}
				if got := g.challenge(second, code); got == http.StatusOK {
					t.Fatal("one TOTP code stepped up a second session after the regeneration")
				}
				var resp struct {
					Codes []string `json:"backup_codes"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || len(resp.Codes) == 0 {
					t.Fatalf("regenerate answer: %s", w.Body.String())
				}
				if got := g.challenge(second, resp.Codes[0]); got != http.StatusOK {
					t.Fatalf("a regenerated backup code = %d, want 200", got)
				}
			})
		}
		t.Run(name+"/disable after the read", func(t *testing.T) {
			g := newRegenRig(t, mk(t))
			g.store.gets, g.store.getAt = 0, handlerRead
			g.store.onGet = func() {
				if w := g.post("/auth/2fa/disable", "", g.main); w.Code != http.StatusOK {
					t.Errorf("disable = %d %s", w.Code, w.Body.String())
				}
			}
			w := g.post("/auth/2fa/backup-codes", "", g.main)
			g.store.mustHaveFired(t)
			if st := g.state(); st != nil {
				t.Fatalf("regeneration resurrected a disabled enrollment (answered %d, enabled=%v)", w.Code, st.Enabled)
			}
			if w.Code == http.StatusOK {
				t.Fatal("regeneration answered 200 with codes for a disabled factor")
			}
		})
		t.Run(name+"/re-enroll after the read", func(t *testing.T) {
			g := newRegenRig(t, mk(t))
			g.store.gets, g.store.getAt = 0, handlerRead
			g.store.onGet = func() {
				if w := g.post("/auth/2fa/disable", "", g.main); w.Code != http.StatusOK {
					t.Errorf("disable = %d %s", w.Code, w.Body.String())
				}
				if w := g.post("/auth/2fa/enroll", "", g.main); w.Code != http.StatusOK {
					t.Errorf("re-enroll = %d %s", w.Code, w.Body.String())
				}
			}
			pending := g.state // read after the hook ran
			w := g.post("/auth/2fa/backup-codes", "", g.main)
			g.store.mustHaveFired(t)
			if w.Code == http.StatusOK {
				t.Fatal("codes asked for under the old secret landed on the new enrollment")
			}
			if st := pending(); st == nil || st.Enabled || len(st.BackupCodes) != 0 {
				t.Fatalf("the pending re-enrollment changed under the regeneration: %v", st != nil && st.Enabled)
			}
		})
	}
}
