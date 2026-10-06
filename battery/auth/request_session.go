package auth

import (
	"errors"
	"net/http"
)

// errNoRequestSession is returned by requestSession when the request carries
// no live session cookie the caller may act on.
var errNoRequestSession = errors.New("no session")

// requestSessions loads the session behind every cookie carrying the session
// name, in the order the client sent them. A jar can hold several (see
// sessionCookieCandidates), and judging only the first is how a gate and the
// handler behind it came to look at different sessions. Token is set from
// the cookie so a caller marks exactly the session it judged.
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

// requestSession picks the session a cookie-driven auth handler acts on.
//
// When an outer middleware already put a principal in the context, only a
// session of that same user qualifies. The handler runs as the principal,
// so judging or acting on another user's cookie lets one user's session
// stand in for the other's: an attacker's JWT next to the victim's
// stepped-up cookie disabled the victim's factor. Without a principal, the
// client's cookie order decides.
//
// preferPending picks a session still waiting on the 2FA challenge first
// (the challenge completes one); otherwise a non-pending session is
// preferred. The caller still checks PendingTwoFactor on the result: when
// only the other kind is present, it is returned.
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
