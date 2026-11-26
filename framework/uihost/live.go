package uihost

import (
	"context"
	"net/http"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"

	"github.com/DonaldMurillo/gofastr/core-ui/island"
	"github.com/DonaldMurillo/gofastr/core/fanout"
)

func (ds *UIHost) handleSSE(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(embedGrantHeader) != "" {
		i18n.HTTPError(w, r, http.StatusUnauthorized, reasonEmbedSSE)
		return
	}
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		i18n.HTTPError(w, r, http.StatusBadRequest, reasonSessionMissing)
		return
	}
	cookieID, ok := ds.verifySessionToken(readSessionCookie(r))
	if !ok || cookieID != sessionID {
		i18n.HTTPError(w, r, http.StatusUnauthorized, reasonUnknownSession)
		return
	}

	identity := island.PresenceIdentityFromContext(r.Context())
	q := r.URL.Query()
	ds.Islands.ServeSSEWithPresence(w, r, identity, island.PresenceTopics{
		Declared:  ds.verifiedDeclaredTopics(q, identity.UserID, time.Now()),
		Requested: island.ParsePresenceTopics(q.Get("presence")),
	})
}

const maxPresenceParamLen = 4096

func boundedPresenceParam(r *http.Request) string {
	v := r.URL.Query().Get("presence")
	if len(v) > maxPresenceParamLen {
		return ""
	}
	return v
}

// SetFanout wires the island manager to a cross-replica fanout so island
// updates reach SSE connections on other replicas.
func (ds *UIHost) SetFanout(f fanout.Fanout) (stop func(), err error) {
	return ds.Islands.SetFanout(f)
}

// SetFanoutContext wires the island manager like [UIHost.SetFanout],
// with the stop of [island.Manager.SetFanoutContext].
func (ds *UIHost) SetFanoutContext(f fanout.Fanout) (stop func(context.Context) error, err error) {
	return ds.Islands.SetFanoutContext(f)
}

// PushUpdate pushes an island update to one session's stream.
func (ds *UIHost) PushUpdate(islandID string, html string, sessionID string) {
	ds.Islands.PushUpdate(island.IslandUpdate{
		IslandID: islandID,
		HTML:     html,
	}, sessionID)
}
