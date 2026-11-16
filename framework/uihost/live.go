package uihost

import (
	"net/http"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"

	"github.com/DonaldMurillo/gofastr/core-ui/island"
	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// handleSSE streams island updates to the client.
func (ds *UIHost) handleSSE(w http.ResponseWriter, r *http.Request) {
	// A grant PRESENTED here is refused, and the refusal says why.
	//
	// SSE is the one item on the "works inside a frame" list that does not.
	// EventSource is the only client of this endpoint and it cannot set a
	// request header, so X-Gofastr-Embed, the frame's ONLY credential, a
	// header precisely so nothing about it is ambient, can never travel on the
	// connection. Moving it into the query string would put a bearer token in
	// access logs, Referer and history, which is the property the header
	// spelling exists to keep. The client half agrees: the embed runtime
	// composition ships no session id and the content route emits no
	// <meta name="gofastr-sse">, so nothing in a frame opens this stream.
	//
	// Refusing outright, rather than falling through to the cookie check
	// below, is the same rule requireSessionOrEmbedGrant states: a presented
	// credential's verdict is final. In a same-site framing the browser really
	// does send the viewer's Strict cookie, and answering on it would stream
	// one identity's island updates into a frame authenticated as another.
	//
	// Passive freshness inside a frame is data-cui-poll / widget Builder.Poll:
	// an ordinary fetch, which the frame's fetch wrapper does put the grant on.
	// See framework/docs/content/embed.md.
	if r.Header.Get(embedGrantHeader) != "" {
		i18n.HTTPError(w, r, http.StatusUnauthorized, reasonEmbedSSE)
		return
	}
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		i18n.HTTPError(w, r, http.StatusBadRequest, reasonSessionMissing)
		return
	}
	// The stream id in the URL is public (it's embedded in page chrome),
	// so possession of an id must prove nothing. The COOKIE token is the
	// credential: it must verify AND its embedded id must match the
	// requested stream. That closes both the old forged-id hole (an
	// attacker-chosen id receiving future PushUpdates) and its sibling
	// the map never closed: subscribing to another user's stream with a
	// leaked-but-real id and no matching cookie.
	cookieID, ok := ds.verifySessionToken(readSessionCookie(r))
	if !ok || cookieID != sessionID {
		i18n.HTTPError(w, r, http.StatusUnauthorized, reasonUnknownSession)
		return
	}

	// Resolve the SERVER-DERIVED identity from the request context (a
	// READ of the middleware-seeded ctx user, never a client param), the
	// page's declared topics (only when their signature verifies for this
	// user) and the bounded ?presence= requests, then delegate to the
	// presence-aware SSE handler, which admits requested topics only
	// through AuthorizeTopic. Anonymous (nil user) is fine: the manager
	// synthesizes a stable per-session pseudo-identity.
	identity := island.PresenceIdentityFromContext(r.Context())
	q := r.URL.Query()
	ds.Islands.ServeSSEWithPresence(w, r, identity, island.PresenceTopics{
		Declared:  ds.verifiedDeclaredTopics(q, identity.UserID, time.Now()),
		Requested: island.ParsePresenceTopics(q.Get("presence")),
	})
}

// maxPresenceParamLen caps the raw ?presence= query value threaded from the
// page URL into the SSE meta tag. The per-topic count/length bounds are
// enforced again by island.ParsePresenceTopics at SSE-connect time; this
// just keeps a hostile page URL from injecting megabytes into the HTML.
const maxPresenceParamLen = 4096

// boundedPresenceParam reads the ?presence= query param from the page request
// and returns it length-capped (not parsed; the SSE handler parses). Empty
// when absent or oversize. This is the value threaded into the SSE meta tag so
// the client's EventSource connection joins the named topic(s).
func boundedPresenceParam(r *http.Request) string {
	v := r.URL.Query().Get("presence")
	if len(v) > maxPresenceParamLen {
		return ""
	}
	return v
}

// SetFanout wires the island manager to a cross-replica fanout so island
// updates reach sessions whose SSE connection lives on another replica.
// framework.WithFanout calls this automatically when the host is mounted
// (via a duck-typed check), so apps normally never call it directly. See
// island.Manager.SetFanout for the delivery semantics (lossy real-time
// lane; the durable lane is the outbox's job).
func (ds *UIHost) SetFanout(f fanout.Fanout) (stop func(), err error) {
	return ds.Islands.SetFanout(f)
}

// PushUpdate pushes an island update for a specific session.
// This is a convenience method that wraps the island manager's push mechanism.
func (ds *UIHost) PushUpdate(islandID string, html string, sessionID string) {
	ds.Islands.PushUpdate(island.IslandUpdate{
		IslandID: islandID,
		HTML:     html,
	}, sessionID)
}
