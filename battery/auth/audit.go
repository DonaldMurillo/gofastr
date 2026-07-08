package auth

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
)

// SecurityEvent is one security-relevant auth occurrence: a login, a 2FA
// lifecycle change, a password reset, an OAuth link, a magic-link issuance.
// It is the unit the audit trail records for non-CRUD auth activity — the
// CRUD hooks already cover user/session row writes, so this struct carries
// only the events those hooks can't see.
type SecurityEvent struct {
	// Kind is the fixed taxonomy identifier (e.g. "login.succeeded",
	// "2fa.enrolled"). Callers MUST use one of the documented kinds so
	// downstream consumers can match on a closed vocabulary.
	Kind string

	// UserID is the resolved principal. Empty when unknown — e.g. a failed
	// login for a nonexistent account, or a password-reset request for an
	// unregistered email. The SQL sink substitutes "-" so the NOT NULL
	// record_id column still accepts the row.
	UserID string

	// Email is the submitted address where relevant. It is the ONLY
	// user-controlled string permitted in any event field; every other
	// Meta value is a fixed-vocabulary token or a count. Useful for
	// forensics; never a credential.
	Email string

	// Remote is the client IP (host part of r.RemoteAddr). XFF is NOT
	// trusted — see the auth threat-model doc. Empty when the request
	// carries no remote address.
	Remote string

	// Meta carries small, fixed-vocabulary details (a "reason" code, a
	// session "count", a provider name). NEVER put secrets, tokens, codes,
	// or any value derived from user input other than Email here.
	Meta map[string]string

	// At is stamped by the emitter when zero, so callers can omit it.
	At time.Time
}

// AuditSink receives security events. Implementations must be fast or buffer
// internally; they are called on the request path. A nil sink disables
// auditing entirely — AuthManager.emitSecurity no-ops.
//
// The built-in NewSQLAuditSink writes one audit_log row per event via
// framework.AppendAuditEvent, sharing the CRUD audit table and its
// sanitisation so the two trails never drift apart.
type AuditSink interface {
	SecurityEvent(ctx context.Context, ev SecurityEvent)
}

// remoteHost returns the host part of r.RemoteAddr without trusting any
// client-supplied X-Forwarded-For. Audit forensics needs the source IP
// literal; the port is per-connection and useless after the fact. Returns
// the raw address verbatim when SplitHostPort can't parse it (no port).
func remoteHost(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// emitSecurity forwards a SecurityEvent to the configured AuditSink. It is
// the single funnel every handler calls, so the nil-sink no-op and the
// panic-isolation live here and callers stay one-liners.
//
// A misbehaving sink must never break auth: the call recovers any panic and
// logs a WARN (the event is lost, but the login/reset/2FA flow it was
// auditing proceeds). At is stamped to now when zero so a handler that
// forgets to set it still produces an ordered trail.
func (m *AuthManager) emitSecurity(ctx context.Context, ev SecurityEvent) {
	sink := m.config.AuditSink
	if sink == nil {
		return
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Warn("auth: audit sink panic recovered; event lost",
					"kind", ev.Kind, "panic", r)
			}
		}()
		sink.SecurityEvent(ctx, ev)
	}()
}

// sqlAuditSink is the built-in AuditSink: it writes each SecurityEvent as a
// row in the framework's audit_log table via framework.AppendAuditEvent.
// entity is always "auth"; op is ev.Kind; record_id is ev.UserID (or "-"
// when empty, since the column is NOT NULL); actor_id is ev.UserID; and the
// diff JSON carries {email, remote, …meta} with empty members omitted.
type sqlAuditSink struct {
	db    *sql.DB
	table string
}

// NewSQLAuditSink builds an AuditSink that writes security events into the
// given audit table (empty → "audit_log", the framework default). It calls
// framework.EnsureAuditTable once at construction so the table exists before
// the first event; a failure here is returned rather than deferred to the
// first request, where a missing table would silently drop events.
//
// One-line wiring:
//
//	sink, err := auth.NewSQLAuditSink(db, "")
//	mgr := auth.New(auth.AuthConfig{ AuditSink: sink, … })
func NewSQLAuditSink(db *sql.DB, table string) (AuditSink, error) {
	if db == nil {
		return nil, fmt.Errorf("auth: NewSQLAuditSink: db is nil")
	}
	if table == "" {
		table = "audit_log"
	}
	if err := framework.EnsureAuditTable(db, table); err != nil {
		return nil, fmt.Errorf("auth: ensure audit table: %w", err)
	}
	return &sqlAuditSink{db: db, table: table}, nil
}

// SecurityEvent writes one audit_log row. A write failure is logged as a
// WARN and swallowed — losing the audit row is preferable to failing the
// auth operation it was recording (a reset that 500s because the audit
// insert failed leaves the password unchanged AND the user locked out).
func (s *sqlAuditSink) SecurityEvent(ctx context.Context, ev SecurityEvent) {
	recordID := ev.UserID
	if recordID == "" {
		recordID = "-"
	}
	diff := make(map[string]any, 2+len(ev.Meta))
	if ev.Email != "" {
		diff["email"] = ev.Email
	}
	if ev.Remote != "" {
		diff["remote"] = ev.Remote
	}
	for k, v := range ev.Meta {
		diff[k] = v
	}
	if err := framework.AppendAuditEvent(ctx, s.db, s.table, "auth", ev.Kind, recordID, ev.UserID, diff); err != nil {
		slog.Warn("auth: failed to write security audit row",
			"kind", ev.Kind, "error", err)
	}
}
