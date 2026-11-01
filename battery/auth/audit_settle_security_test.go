package auth

import (
	"context"
	"database/sql"
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
	deadline  bool
	cancelled bool
}

func (s *ctxRecordingSink) SecurityEvent(ctx context.Context, _ SecurityEvent) {
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
