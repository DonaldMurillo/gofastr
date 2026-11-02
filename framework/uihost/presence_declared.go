package uihost

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/island"
)

// PresenceScreen is an optional interface a screen implements to put its
// viewers on presence topics. The host asks the loaded instance (after
// SetParams and Load), so a detail screen can name the record it shows:
//
//	func (s *DocScreen) PresenceTopics(ctx context.Context) []string {
//		return []string{"doc:" + s.doc.ID}
//	}
//
// The topics are signed into the page's stream URL for the viewer the page
// was rendered for, and the stream joins them with no AuthorizeTopic hook:
// rendering the page for that viewer was the authorization. A topic the
// browser names itself (?presence=) is refused unless a hook admits it. A
// topic holding a comma, or more than 128 bytes, is dropped, and at most 16
// survive. See framework/docs/content/presence.md.
type PresenceScreen interface {
	PresenceTopics(ctx context.Context) []string
}

// declaredPresenceTTL is how long a signed topic list stays good: the
// session token's own lifetime, so a tab that outlives it has re-rendered
// anyway.
const declaredPresenceTTL = sessionMaxAge

// declaredPresenceLabel separates the presence MAC key from the session
// token's use of the same secret.
const declaredPresenceLabel = "gofastr presence topics v1"

// declaredTopics returns the topics comp declares for this render, bounded
// the way the stream bounds them. comp is the instance that rendered: under
// RenderAlt that is the alt, so a refused screen's topics never ride the page
// that refused it.
func declaredTopics(ctx context.Context, comp component.Component) []string {
	ps, ok := comp.(PresenceScreen)
	if !ok {
		return nil
	}
	kept := make([]string, 0, 4)
	for _, t := range ps.PresenceTopics(ctx) {
		if !strings.Contains(t, ",") {
			kept = append(kept, t)
		}
	}
	return island.ParsePresenceTopics(strings.Join(kept, ","))
}

// declaredPresenceQuery is the stream-URL suffix carrying comp's declared
// topics and their signature, or "" when it declares none.
func (ds *UIHost) declaredPresenceQuery(ctx context.Context, comp component.Component) string {
	topics := declaredTopics(ctx, comp)
	if len(topics) == 0 {
		return ""
	}
	list := strings.Join(topics, ",")
	userID := island.PresenceIdentityFromContext(ctx).UserID
	sig := ds.signDeclaredTopics(userID, list, time.Now().Add(declaredPresenceTTL))
	return "&declared=" + url.QueryEscape(list) + "&dsig=" + url.QueryEscape(sig)
}

// signDeclaredTopics signs list for userID until exp with the current
// session key. The signature does not name the stream id: the runtime
// re-mints that in place (sse.js remintSession) and the list must survive.
// Binding the user instead keeps one viewer's list from admitting another.
func (ds *UIHost) signDeclaredTopics(userID, list string, exp time.Time) string {
	ds.mu.RLock()
	key := ds.sessionKey
	ds.mu.RUnlock()
	e := strconv.FormatInt(exp.Unix(), 10)
	return e + "." + base64.RawURLEncoding.EncodeToString(declaredMAC(key, userID, list, e))
}

// verifiedDeclaredTopics returns the declared topics q carries when their
// signature verifies for userID under the current or a previous session
// key and has not expired; nil otherwise.
func (ds *UIHost) verifiedDeclaredTopics(q url.Values, userID string, now time.Time) []string {
	list, sig := q.Get("declared"), q.Get("dsig")
	if list == "" || len(list) > maxPresenceParamLen {
		return nil
	}
	e, mac, ok := strings.Cut(sig, ".")
	if !ok {
		return nil
	}
	exp, err := strconv.ParseInt(e, 10, 64)
	if err != nil || now.Unix() >= exp {
		return nil
	}
	got, err := base64.RawURLEncoding.DecodeString(mac)
	if err != nil {
		return nil
	}
	ds.mu.RLock()
	keys := append([][]byte{ds.sessionKey}, ds.previousSessionKeys...)
	ds.mu.RUnlock()
	for _, key := range keys {
		if hmac.Equal(got, declaredMAC(key, userID, list, e)) {
			return island.ParsePresenceTopics(list)
		}
	}
	return nil
}

// declaredMAC is HMAC-SHA256 under a key derived from the session key for
// this one use. Every field but the last is length-prefixed, so no two
// (user, list, expiry) triples share an input.
func declaredMAC(sessionKey []byte, userID, list, exp string) []byte {
	k := hmac.New(sha256.New, sessionKey)
	k.Write([]byte(declaredPresenceLabel))
	m := hmac.New(sha256.New, k.Sum(nil))
	m.Write([]byte(strconv.Itoa(len(userID)) + ":" + userID + strconv.Itoa(len(exp)) + ":" + exp + list))
	return m.Sum(nil)
}
