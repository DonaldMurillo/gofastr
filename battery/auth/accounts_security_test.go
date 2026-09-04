package auth

// Atomicity of the unlink last-credential guard.
//
// Property: the last-credential guard on /auth/unlink holds under
// concurrency — two concurrent unlinks of a 2-method OAuth-only account
// cannot strip it to zero login methods. The single-request half of the
// invariant is pinned in accounts_test.go (RefusesLast,
// OAuthOnlyUserCannotUnlink); this file extends it to concurrent
// presentation. The guard rides the AtomicUnlinker store seam
// (UnlinkOAuthGuarded), which decides and deletes in one operation, so
// exactly one of two racing unlinks can win.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// rendezvousUnlinkStore pins the old list→delete race window open
// deterministically: when armed, the FIRST ListAccounts call blocks until
// the SECOND arrives. Under the atomic path the handler never consults
// ListAccounts for the guard, so the rendezvous simply never fires; under
// a regression to check-then-act, both handlers hold the 2-link snapshot
// before either UnlinkOAuth can run — no scheduling luck involved.
type rendezvousUnlinkStore struct {
	*linkingStore
	armed     atomic.Bool
	entered   atomic.Int64
	release   chan struct{}
	closeOnce sync.Once
}

func (s *rendezvousUnlinkStore) ListAccounts(ctx context.Context, userID string) ([]Account, error) {
	accts, err := s.linkingStore.ListAccounts(ctx, userID)
	if err != nil {
		return nil, err
	}
	if s.armed.Load() {
		if s.entered.Add(1) == 1 {
			select {
			case <-s.release:
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(10 * time.Second):
				// Fall through rather than hang forever; the assertions
				// below report whatever actually happened.
			}
		} else {
			s.closeOnce.Do(func() { close(s.release) })
		}
	}
	return accts, nil
}

func TestUnlinkAtomicMethodCount(t *testing.T) {
	ctx := context.Background()
	store := newLinkingStore()
	user, err := store.CreateUser(ctx, "race@example.com", "", []string{"user"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.LinkOAuth(ctx, user.GetID(), "google", "g-1"); err != nil {
		t.Fatalf("LinkOAuth google: %v", err)
	}
	if err := store.LinkOAuth(ctx, user.GetID(), "github", "gh-1"); err != nil {
		t.Fatalf("LinkOAuth github: %v", err)
	}
	rendezvous := &rendezvousUnlinkStore{linkingStore: store, release: make(chan struct{})}

	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     rendezvous,
		DevMode:       true,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewAccountsPlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	sess, err := mgr.SessionStore().Create(ctx, user.GetID(), time.Hour)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)

	// Positive control: one ordinary unlink of a 3-method account succeeds
	// (remaining=2 satisfies the guard), proving the harness drives the
	// handler end to end before the race is armed.
	if err := store.LinkOAuth(ctx, user.GetID(), "gitlab", "gl-1"); err != nil {
		t.Fatalf("LinkOAuth gitlab: %v", err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/auth/unlink/gitlab", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sess.Token})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("setup: single unlink of a 3-method account got %d (body=%s), want 200 — harness broken, not the seam", w.Code, w.Body.String())
	}

	// The race: both remaining unlinks fire concurrently.
	rendezvous.armed.Store(true)
	outcomes := make(chan int, 2)
	for _, provider := range []string{"google", "github"} {
		go func(provider string) {
			req := httptest.NewRequest(http.MethodDelete, "/auth/unlink/"+provider, nil)
			req.AddCookie(&http.Cookie{Name: "session_id", Value: sess.Token})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			outcomes <- w.Code
		}(provider)
	}
	codes := []int{<-outcomes, <-outcomes}

	succeeded := 0
	for _, c := range codes {
		if c == http.StatusOK {
			succeeded++
		}
	}
	remaining, err := store.ListAccounts(ctx, user.GetID())
	if err != nil {
		t.Fatalf("post-race ListAccounts: %v", err)
	}
	if succeeded == 2 && len(remaining) == 0 {
		t.Errorf("SECURITY: [unlink-race] both concurrent unlinks of a 2-method OAuth-only account "+
			"succeeded (statuses %v) and the account retains 0 login methods — the refuse-the-last "+
			"invariant must be decided inside the store operation, not by a check-then-act "+
			"ListAccounts→UnlinkOAuth pair", codes)
	}
}
