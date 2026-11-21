package auth

import (
	"errors"
	"net/http"
)

var errNoRequestSession = errors.New("no session")

func (m *AuthManager) requestSessions(r *http.Request) []*Session {
	var out []*Session
	for _, token := range sessionCookieCandidates(r, m.Config().SessionCookie) {
		sess, err := m.SessionStore().Get(r.Context(), token)
		if err != nil || sess == nil {
			continue
		}
		s := *sess
		s.Token = token
		out = append(out, &s)
	}
	return out
}

func (m *AuthManager) requestSession(r *http.Request, preferPending bool) (*Session, error) {
	sessions := m.requestSessions(r)
	if u := GetCurrentUser(r.Context()); u != nil {
		id := u.GetID()
		mine := sessions[:0]
		for _, s := range sessions {
			if s.UserID == id {
				mine = append(mine, s)
			}
		}
		sessions = mine
	}
	if len(sessions) == 0 {
		return nil, errNoRequestSession
	}
	for _, s := range sessions {
		if s.PendingTwoFactor == preferPending {
			return s, nil
		}
	}
	return sessions[0], nil
}
