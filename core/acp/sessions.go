package acp

import (
	"fmt"
)

// defaultSessionsPerConn is the live-session cap one connection gets
// when Options.MaxSessions is left at zero. ACP clients legitimately
// mint one session per task over a long-lived connection (an editor
// session can run for hours), so the bound is generous — but it exists:
// session/new is as cheap as a frame gets (authenticated only when the
// embedder wires Options.Authenticate, and the zero-value Options is
// explicitly usable), and without a bound one connected client grows
// the process monotonically for the life of the connection. Found red
// in the 2026-09-05 adversarial pass round 4 (10,000 session/new frames
// on one connection, all accepted).
const defaultSessionsPerConn = 1024

// SessionOverflowPolicy selects what a connection at Options.MaxSessions
// does with its next session/new.
type SessionOverflowPolicy uint8

const (
	// SessionOverflowRefuse answers the session/new (or session/load)
	// with an ErrInvalidParams error, mirroring the 4 MiB frame cap
	// Serve already enforces. The default.
	SessionOverflowRefuse SessionOverflowPolicy = iota
	// SessionOverflowEvictOldest drops the connection's oldest session
	// (canceling its in-flight prompt turn, if any) and seats the new
	// one. For agent hosts whose clients churn sessions faster than
	// they close them.
	SessionOverflowEvictOldest
)

// sessionCapFor resolves the configured cap; negative means unlimited.
func (s *Server) sessionCapFor() int {
	if s.opts.MaxSessions == 0 {
		return defaultSessionsPerConn
	}
	return s.opts.MaxSessions
}

// refuseFullSessions returns the invalid-params error a session mint
// past the cap must answer, or nil when a seat is available (directly,
// or via eviction once the new session exists). Handlers call it BEFORE
// invoking the embedder's Agent, so a refused frame never makes the
// embedder mint a session that cannot be seated.
func (st *serverState) refuseFullSessions(existingID string) *wireRespError {
	max := st.srv.sessionCapFor()
	if max <= 0 {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, exists := st.sessions[existingID]; exists {
		return nil // a reload replaces its own entry; no new seat
	}
	if len(st.sessions) < max {
		return nil
	}
	if st.srv.opts.SessionOverflow == SessionOverflowEvictOldest && len(st.sessionOrder) > 0 {
		return nil
	}
	return &wireRespError{
		Code:    ErrInvalidParams,
		Message: fmt.Sprintf("session limit reached: at most %d live sessions per connection", max),
	}
}

// recordSession applies the per-connection session cap and inserts s
// under id. A reload of an id this connection already holds replaces
// the entry without growing the set. Past the cap the policy applies:
// refuse (nil, non-nil error) or evict the oldest session — its
// in-flight prompt, if any, is canceled so the turn ends promptly.
// The pre-check above already refused the hopeless frames; this is the
// authoritative check under the lock.
func (st *serverState) recordSession(s *session, id string) *wireRespError {
	max := st.srv.sessionCapFor()
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, exists := st.sessions[id]; exists {
		st.sessions[id] = s
		return nil
	}
	if max > 0 && len(st.sessions) >= max {
		if st.srv.opts.SessionOverflow != SessionOverflowEvictOldest || len(st.sessionOrder) == 0 {
			return &wireRespError{
				Code:    ErrInvalidParams,
				Message: fmt.Sprintf("session limit reached: at most %d live sessions per connection", max),
			}
		}
		oldestID := st.sessionOrder[0]
		st.sessionOrder = st.sessionOrder[1:]
		if oldest, ok := st.sessions[oldestID]; ok {
			delete(st.sessions, oldestID)
			oldest.mu.Lock()
			cancel := oldest.cancel
			oldest.mu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
	}
	st.sessions[id] = s
	st.sessionOrder = append(st.sessionOrder, id)
	return nil
}
