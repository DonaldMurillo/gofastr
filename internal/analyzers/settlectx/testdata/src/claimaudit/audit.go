package claimaudit

import (
	"context"
	"net/http"
	"time"

	"lib"
)

type SecurityEvent struct{ Kind, UserID string }

// AuditSink is the host's sink (battery/auth.AuditSink).
type AuditSink interface {
	SecurityEvent(ctx context.Context, ev SecurityEvent)
}

type Store interface {
	UpdateRole(ctx context.Context, id, role string) error
	Lookup(ctx context.Context, cred string) (string, error)
}

type Manager struct {
	sink  AuditSink
	store Store
}

// emitSecurity is battery/auth before 79cc5197: the sink gets the
// caller's context as is. Reported here, once, where the fix goes.
func (m *Manager) emitSecurity(ctx context.Context, ev SecurityEvent) { // want emitSecurity:`audits\[0\]`
	m.sink.SecurityEvent(ctx, ev) // want `SecurityEvent writes the audit event on its caller's request context \(audit.go:39 hands it r.Context\(\) after a change\)`
}

func (m *Manager) changeRole(w http.ResponseWriter, r *http.Request) {
	if err := m.store.UpdateRole(r.Context(), "u", "admin"); err != nil {
		return
	}
	// A second call site after a change: still one report, at the sink.
	m.emitSecurity(r.Context(), SecurityEvent{Kind: "role.changed"})
	_ = m.store.UpdateRole(r.Context(), "u", "user")
	m.emitSecurity(r.Context(), SecurityEvent{Kind: "role.changed"})
}

// emitTokenEvent is the token middleware's emitter before 9eaba945: the
// sink is a parameter, the context the second argument.
func emitTokenEvent(sink AuditSink, ctx context.Context, r *http.Request, kind string) { // want emitTokenEvent:`audits\[1\]`
	sink.SecurityEvent(ctx, SecurityEvent{Kind: kind}) // want `SecurityEvent writes the audit event on its caller's request context \(audit.go:53 hands it r.Context\(\) after a change\)`
}

func (m *Manager) middleware(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, err := m.store.Lookup(ctx, "cred"); err != nil {
		emitTokenEvent(m.sink, ctx, r, "token.auth_failed")
	}
}

// emitDetached is 79cc5197's own spelling, before d799883c: the hang-up
// no longer cancels the write, but a host sink that blocks now holds the
// auth handler as long as its remote does.
func (m *Manager) emitDetached(ctx context.Context, ev SecurityEvent) {
	m.sink.SecurityEvent(context.WithoutCancel(ctx), ev) // want `SecurityEvent hands a host AuditSink context.WithoutCancel with no timeout`
}

// deliverAudit is the fix (9eaba945/d799883c): detached and bounded. It
// carries no audits fact, so its callers are quiet.
func deliverAudit(ctx context.Context, sink AuditSink, ev SecurityEvent) {
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	sink.SecurityEvent(sctx, ev)
}

func (m *Manager) changeRoleFixed(w http.ResponseWriter, r *http.Request) {
	if err := m.store.UpdateRole(r.Context(), "u", "admin"); err != nil {
		return
	}
	deliverAudit(r.Context(), m.sink, SecurityEvent{Kind: "role.changed"}) // quiet: the helper detaches and bounds
}

// An audit with no change before it on the request's context records the
// attempt itself; there is nothing for a hang-up to orphan: quiet.
type quietSink struct{ sink AuditSink }

func (q *quietSink) attempt(w http.ResponseWriter, r *http.Request) {
	q.sink.SecurityEvent(r.Context(), SecurityEvent{Kind: "login.attempt"})
}

// Direct sink call in a handler after a change: reported at the call.
type Auditor interface {
	Record(ctx context.Context, what string) error
}

type admin struct {
	audit Auditor
	store Store
}

func (a *admin) disable2FA(w http.ResponseWriter, r *http.Request) {
	_ = a.store.UpdateRole(r.Context(), "u", "no2fa")
	_ = a.audit.Record(r.Context(), "2fa.disabled") // want `Record writes the audit event on the request's context after the change on line 98`
}

// Through an imported helper with an audits fact: reported at the call.
func (a *admin) viaLib(w http.ResponseWriter, r *http.Request, sink lib.Sink) {
	_ = a.store.UpdateRole(r.Context(), "u", "admin")
	lib.Emit(r.Context(), sink, "role.changed") // want `Emit writes the audit event on the request's context after the change on line 104`
}

// The helper's context never comes from a request (a background job's
// own context): quiet.
func (m *Manager) job(ctx context.Context) { // want job:`audits\[0\]`
	_ = m.store.UpdateRole(ctx, "u", "x")
	m.sink.SecurityEvent(ctx, SecurityEvent{Kind: "job"})
}

// A non-audit host interface (the store) handed a request context is
// not an audit: quiet. And an unexported seam handed an unbounded
// detach is the package's own driver wrapper, not a host: quiet.
type execer interface {
	ExecContext(ctx context.Context, q string) error
}

func pragmaOff(ctx context.Context, e execer) {
	_ = e.ExecContext(context.WithoutCancel(ctx), "PRAGMA x=OFF")
}

// A chain of helpers reports once, at the innermost sink call, not at
// each hop (emitSecurity → deliverAudit → sink before the fix).
func (m *Manager) emitVia(ctx context.Context, ev SecurityEvent) { // want emitVia:`audits\[0\]`
	m.deliver(ctx, ev)
}

func (m *Manager) deliver(ctx context.Context, ev SecurityEvent) { // want deliver:`audits\[0\]`
	m.sink.SecurityEvent(ctx, ev) // want `SecurityEvent writes the audit event on its caller's request context \(audit.go:\d+ hands it`
}

func (m *Manager) chained(w http.ResponseWriter, r *http.Request) {
	_ = m.store.UpdateRole(r.Context(), "u", "admin")
	m.emitVia(r.Context(), SecurityEvent{Kind: "role.changed"})
}
