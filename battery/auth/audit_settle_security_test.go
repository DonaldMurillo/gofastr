package auth

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Found by the 2026-11-01 battery/auth bug bash.

type cancelAfterRoles struct {
	*EntityUserStore
	cancel context.CancelFunc
}

func (c cancelAfterRoles) UpdateRoles(ctx context.Context, id string, roles []string) error {
	err := c.EntityUserStore.UpdateRoles(ctx, id, roles)
	c.cancel() // the client hangs up after the change committed
	return err
}

// The audit row was written on the request's context after the change it
// records, so a client that hung up in between dropped the row: a stolen
// session could change roles or disable 2FA and leave no trace.
func TestAuditRowSurvivesCancelledRequest(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	users := NewEntityUserStore(db, "users")
	if err := users.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	sink, err := NewSQLAuditSink(db, "")
	if err != nil {
		t.Fatal(err)
	}
	reqCtx, cancel := context.WithCancel(ctx)
	mgr := New(AuthConfig{JWTSecret: "x", DevMode: true, UserStore: cancelAfterRoles{users, cancel}, AuditSink: sink})
	u, err := users.CreateUser(ctx, "v@example.com", "h", []string{"user"})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.SetUserRoles(reqCtx, u.GetID(), []string{"admin"}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE op = 'roles.updated'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("SECURITY: privilege change committed with %d roles.updated audit rows, want 1", n)
	}
}

// A row with no actor is out of erasure's reach, so it must not hold the
// address; a row with an actor keeps it (erasure scrubs that one).
func TestAuditActorlessRowHoldsNoAddress(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	sink, err := NewSQLAuditSink(db, "")
	if err != nil {
		t.Fatal(err)
	}
	sink.SecurityEvent(ctx, SecurityEvent{Kind: "magiclink.requested", Email: "victim@example.com", Remote: "203.0.113.9"})
	sink.SecurityEvent(ctx, SecurityEvent{Kind: "login.succeeded", UserID: "u1", Email: "owner@example.com"})
	rows, err := db.Query(`SELECT op, diff FROM audit_log ORDER BY op`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var op string
		var diff sql.NullString
		if err := rows.Scan(&op, &diff); err != nil {
			t.Fatal(err)
		}
		switch op {
		case "magiclink.requested":
			if strings.Contains(diff.String, "victim@example.com") || !strings.Contains(diff.String, hashedIdentifier("victim@example.com")) {
				t.Errorf("actor-less row diff = %s, want email_hash and no address", diff.String)
			}
		case "login.succeeded":
			if !strings.Contains(diff.String, "owner@example.com") {
				t.Errorf("actor row lost its address: %s", diff.String)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

type ctxRecordingSink struct {
	called    bool
	deadline  bool
	cancelled bool
}

func (s *ctxRecordingSink) SecurityEvent(ctx context.Context, _ SecurityEvent) {
	s.called = true
	_, s.deadline = ctx.Deadline()
	s.cancelled = ctx.Err() != nil
}

// A host sink gets the request's values without its cancellation, and a
// deadline: emitSecurity is synchronous, so a sink with no bound held the
// auth handler as long as its remote did (2026-11-01 review).
func TestAuditCustomSinkContextBounded(t *testing.T) {
	sink := &ctxRecordingSink{}
	mgr := New(AuthConfig{JWTSecret: "x", DevMode: true, AuditSink: sink})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mgr.emitSecurity(ctx, SecurityEvent{Kind: "login.succeeded"})
	if sink.cancelled {
		t.Error("the sink saw the request's cancellation")
	}
	if !sink.deadline {
		t.Error("the sink's context has no deadline")
	}
}

// The token middleware called its sink directly on the request's context,
// so a replayed token whose sender hung up left no token.auth_failed row,
// and a host sink got no deadline (2026-11-01 review, round 2).
func TestTokenAuditGoesThroughDeliverAudit(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	sink := &ctxRecordingSink{}
	users := &staticUserStore{byID: map[string]User{}}
	ctx, cancel := context.WithCancel(context.Background())
	store := cancelAfterFind{APITokenStore: ts, cancel: cancel}
	h := TokenMiddleware(users, nil, store, WithTokenAudit(sink))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := bearerRequest(http.MethodGet, "/x", "gfsk_"+strings.Repeat("0", 40), "").WithContext(ctx)
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !sink.called {
		t.Fatal("no token.auth_failed event")
	}
	if sink.cancelled || !sink.deadline {
		t.Errorf("token audit context: cancelled=%v deadline=%v, want the request's values minus its cancellation, with a deadline", sink.cancelled, sink.deadline)
	}
}

// email_hash is taken over the canonical address, so spellings of one
// address correlate.
func TestAuditEmailHashIsCanonical(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	sink, err := NewSQLAuditSink(db, "")
	if err != nil {
		t.Fatal(err)
	}
	sink.SecurityEvent(context.Background(), SecurityEvent{Kind: "magiclink.requested", Email: "  Victim@Example.COM "})
	var diff string
	if err := db.QueryRow(`SELECT diff FROM audit_log`).Scan(&diff); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, hashedIdentifier("victim@example.com")) {
		t.Errorf("email_hash of a non-canonical spelling = %s, want the canonical address's digest", diff)
	}
}

// cancelAfterFind hangs the client up once the token lookup has answered.
type cancelAfterFind struct {
	APITokenStore
	cancel context.CancelFunc
}

func (c cancelAfterFind) FindByHash(ctx context.Context, hash string) (*APIToken, error) {
	t, err := c.APITokenStore.FindByHash(ctx, hash)
	c.cancel()
	return t, err
}
