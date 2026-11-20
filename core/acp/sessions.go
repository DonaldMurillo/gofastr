package acp

import (
	"fmt"
)

const defaultSessionsPerConn = 1024

// SessionOverflowPolicy selects what a connection at Options.MaxSessions does
// with its next session/new.
type SessionOverflowPolicy uint8

const (
	// SessionOverflowRefuse answers the overflowing call ErrInvalidParams.
	SessionOverflowRefuse SessionOverflowPolicy = iota
	// SessionOverflowEvictOldest drops the connection's oldest session
	// (canceling its in-flight prompt turn, if any) and seats the new one.
	SessionOverflowEvictOldest
)

func (s *Server) sessionCapFor() int {
	if s.opts.MaxSessions == 0 {
		return defaultSessionsPerConn
	}
	return s.opts.MaxSessions
}

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

func (st *serverState) recordSession(s *session, id string, fresh bool) *wireRespError {
	max := st.srv.sessionCapFor()
	st.mu.Lock()
	defer st.mu.Unlock()
	if fresh {
		_, live := st.sessions[id]
		_, draining := st.draining[id]
		if live || draining {
			return &wireRespError{Code: ErrInternalError, Message: "session/new: agent returned a session ID this connection already holds"}
		}
	}
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
			if oldest.busy {
				st.draining[oldestID] = oldest
			}
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

func (st *serverState) sessionBusy(id string) bool {
	st.mu.Lock()
	sessions := []*session{st.sessions[id], st.draining[id]}
	st.mu.Unlock()
	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		sess.mu.Lock()
		busy := sess.busy
		sess.mu.Unlock()
		if busy {
			return true
		}
	}
	return false
}
