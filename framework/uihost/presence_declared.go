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

// PresenceScreen is implemented by a screen that puts its viewers on
// presence topics. The host asks the loaded instance and signs the topics
// into the page's stream URL for the viewer it rendered for.
type PresenceScreen interface {
	PresenceTopics(ctx context.Context) []string
}

const declaredPresenceTTL = sessionMaxAge

const declaredPresenceLabel = "gofastr presence topics v1"

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

func (ds *UIHost) signDeclaredTopics(userID, list string, exp time.Time) string {
	ds.mu.RLock()
	key := ds.sessionKey
	ds.mu.RUnlock()
	e := strconv.FormatInt(exp.Unix(), 10)
	return e + "." + base64.RawURLEncoding.EncodeToString(declaredMAC(key, userID, list, e))
}

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

func declaredMAC(sessionKey []byte, userID, list, exp string) []byte {
	k := hmac.New(sha256.New, sessionKey)
	k.Write([]byte(declaredPresenceLabel))
	m := hmac.New(sha256.New, k.Sum(nil))
	m.Write([]byte(strconv.Itoa(len(userID)) + ":" + userID + strconv.Itoa(len(exp)) + ":" + exp + list))
	return m.Sum(nil)
}
