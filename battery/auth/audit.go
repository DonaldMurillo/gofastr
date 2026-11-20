package auth

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework"
)

// SecurityEvent is one auth security event for the audit trail.
type SecurityEvent struct {
	// Kind is one of the closed vocabulary listed in audit-log.md.
	Kind string

	// UserID is the principal, or "" when unknown.
	UserID string

	// Email is the submitted address, the only user-supplied value an event may carry.
	Email string

	emailCanonical string

	// Remote is the host part of r.RemoteAddr; X-Forwarded-For is never read.
	Remote string

	// Meta holds fixed-vocabulary details and counts, never secrets.
	Meta map[string]string

	// At is stamped by the emitter when zero, so callers can omit it.
	At time.Time
}

// AuditSink receives security events on a context with the request's values, no cancellation and a five-second deadline.
type AuditSink interface {
	SecurityEvent(ctx context.Context, ev SecurityEvent)
}

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

func (m *AuthManager) emitSecurity(ctx context.Context, ev SecurityEvent) {
	sink := m.config.AuditSink
	if sink == nil {
		return
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	if ev.Email != "" {
		if c, err := m.canonicalizeEmail(ev.Email); err == nil {
			ev.Email, ev.emailCanonical = c, sha256hex(c)
		}
	}
	deliverAudit(ctx, sink, ev)
}

func deliverAudit(ctx context.Context, sink AuditSink, ev SecurityEvent) {
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("auth: audit sink panic recovered; event lost",
				"kind", ev.Kind, "panic", textsafe.Recovered(r))
		}
	}()
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), auditWriteTimeout)
	defer cancel()
	sink.SecurityEvent(sctx, ev)
}

const auditWriteTimeout = 5 * time.Second

type sqlAuditSink struct {
	db    *sql.DB
	table string
}

// NewSQLAuditSink returns an AuditSink writing to table (default audit_log), creating it first.
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

func (s *sqlAuditSink) SecurityEvent(ctx context.Context, ev SecurityEvent) {
	recordID := ev.UserID
	if recordID == "" {
		recordID = "-"
	}
	diff := make(map[string]any, 2+len(ev.Meta))
	switch {
	case ev.Email == "":
	case ev.UserID != "":
		diff["email"] = ev.Email
	default:
		addr := ev.Email
		if ev.emailCanonical == "" || ev.emailCanonical != sha256hex(addr) {
			if c, err := CanonicalEmail(addr); err == nil {
				addr = c
			}
		}
		diff["email_hash"] = hashedIdentifier(addr)
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
