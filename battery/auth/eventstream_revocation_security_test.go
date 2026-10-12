package auth

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// An open _events stream re-validates the principal that opened it, not only
// the RBAC answer frozen into its request context. Revoking the session,
// revoking the API token, deleting the JWT's user, or dropping the role must
// close the stream at the next check, the way a fresh request with the same
// credentials is refused. Before the principal-check seam the stream kept
// delivering every later write to a caller the static routes already
// answered 401 or 403.
func TestEventStreamClosesOnRevocation(t *testing.T) {
	cases := []struct {
		name     string
		readPerm string
		cred     string // "session", "token", "jwt"
		revoke   string // "session", "role", "token", "user"
	}{
		{"session-deleted", "", "session", "session"},
		{"session-deleted-access", "docs:read", "session", "session"},
		{"role-dropped", "docs:read", "session", "role"},
		{"token-revoked", "", "token", "token"},
		{"jwt-user-deleted", "", "jwt", "user"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mgr, store := newTestManager(t)
			user := seedUser(t, store, "reader@example.com", "password123")

			db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "events.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			if _, err := db.Exec(`CREATE TABLE docs (id TEXT PRIMARY KEY, body TEXT)`); err != nil {
				t.Fatal(err)
			}
			cfg := entity.EntityConfig{Fields: []schema.Field{{Name: "body", Type: schema.String}}}
			if c.readPerm != "" {
				cfg.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Read: c.readPerm}}
			}
			ent := entity.Define("docs", cfg.WithTimestamps(false))
			ch := crud.NewCrudHandler(ent, db).WithJSONCase(crud.CaseSnake).WithEventStreamReauth(time.Second)
			ch.Events = event.NewEventBus()

			var mu sync.Mutex
			assigned := map[string][]string{user.GetID(): {"reader"}}
			rolesFn := func(ctx context.Context) []string {
				u := GetCurrentUser(ctx)
				if u == nil {
					return nil
				}
				mu.Lock()
				defer mu.Unlock()
				return append([]string(nil), assigned[u.GetID()]...)
			}
			policy := access.NewRolePolicy()
			if err := policy.Grant("reader", "docs:read"); err != nil {
				t.Fatal(err)
			}

			var authn func(http.Handler) http.Handler
			var revoke func()
			req := httptest.NewRequest(http.MethodGet, "/docs/_events", nil)
			switch c.cred {
			case "session":
				sess, err := mgr.SessionStore().Create(context.Background(), user.GetID(), time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				req.AddCookie(&http.Cookie{Name: mgr.Config().SessionCookie, Value: sess.Token})
				authn = SessionMiddleware(mgr)
				revoke = func() {
					if err := mgr.SessionStore().Delete(context.Background(), sess.Token); err != nil {
						t.Fatal(err)
					}
				}
			case "token":
				tokens, err := NewSQLAPITokenStore(db)
				if err != nil {
					t.Fatal(err)
				}
				tok, rec, err := IssueToken(context.Background(), tokens, TokenSpec{Name: "t", OwnerKind: OwnerKindUser, OwnerID: user.GetID(), Scopes: []string{"docs:read"}})
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "Bearer "+tok)
				authn = TokenMiddleware(store, nil, tokens)
				revoke = func() {
					if err := tokens.Revoke(context.Background(), rec.ID, OwnerKindUser, user.GetID()); err != nil {
						t.Fatal(err)
					}
				}
			case "jwt":
				tok, err := mgr.JWT().GenerateToken(user)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "Bearer "+tok)
				authn = RequireAuth(mgr.JWT())
				revoke = func() {
					store.mu.Lock()
					delete(store.byID, user.GetID())
					delete(store.users, user.GetEmail())
					store.mu.Unlock()
				}
			}
			if c.revoke == "role" {
				revoke = func() {
					mu.Lock()
					assigned[user.GetID()] = nil
					mu.Unlock()
				}
			}
			streamH := authn(access.Middleware(policy, rolesFn)(ch.EventStream()))

			sctx, cancel := context.WithCancel(req.Context())
			defer cancel()
			rec := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { streamH.ServeHTTP(rec, req.WithContext(sctx)); close(done) }()
			time.Sleep(200 * time.Millisecond)

			ch.EmitEvent(context.Background(), event.EntityCreated, map[string]any{"id": "d-before", "body": "before-revoke"})
			time.Sleep(200 * time.Millisecond)
			revoke()
			ch.EmitEvent(context.Background(), event.EntityCreated, map[string]any{"id": "d-after", "body": "after-revoke-secret"})

			select {
			case <-done:
			case <-time.After(3 * time.Second):
				cancel()
				<-done
				t.Errorf("SECURITY: stream still open 3s after the %s was revoked", c.revoke)
			}
			body := rec.Body.String()
			if !strings.Contains(body, "before-revoke") {
				t.Fatalf("stream never delivered the pre-revocation write: %q", body)
			}
			if strings.Contains(body, "after-revoke-secret") {
				t.Errorf("SECURITY: stream delivered a write after the %s was revoked: %q", c.revoke, body)
			}
		})
	}
}
