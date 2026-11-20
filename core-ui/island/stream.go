package island

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/internal/clientip"
)

type ssePayload struct {
	Island string `json:"island"`
	HTML   string `json:"html"`
}

const maxSessionIDLen = 256

// ServeSSE is an http.HandlerFunc that streams island updates to a client.
func (m *Manager) ServeSSE(w http.ResponseWriter, r *http.Request) {
	m.ServeSSEWithPresence(w, r, PresenceIdentity{}, PresenceTopics{})
}

// ServeSSEWithPresence streams island updates AND registers the connection on one or more presence
// topics for the duration of the SSE stream.
func (m *Manager) ServeSSEWithPresence(w http.ResponseWriter, r *http.Request, identity PresenceIdentity, pt PresenceTopics) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		i18n.HTTPError(w, r, http.StatusBadRequest, reasonSessionMissing)
		return
	}
	if len(sessionID) > maxSessionIDLen {
		i18n.HTTPError(w, r, http.StatusBadRequest, reasonSessionTooLong)
		return
	}

	sse := stream.NewSSEWriter(w)

	topics := m.admitTopics(r.Context(), pt)

	ch, cancel, err := m.ConnectSession(sessionID, m.ClientKey(r, identity))
	if err != nil {
		w.Header().Set("Retry-After", "1")
		i18n.HTTPErrorFor(w, r, http.StatusTooManyRequests, err)
		if err == ErrPeerStreamLimit {
			m.hintUntrustedProxy(r)
		}
		return
	}
	defer cancel()
	handle := m.PresenceJoin(sessionID, identity, topics)
	defer handle.Leave()

	if err := sse.WriteComment("connected"); err != nil {
		return
	}

	reqCtx := r.Context()
	streamCtx, streamCancel := context.WithCancel(context.Background())
	defer streamCancel()
	go func() {
		<-reqCtx.Done()
		if reqCtx.Err() == context.Canceled {
			streamCancel()
		}
	}()

	held := func() bool {
		fresh, ok := handler.RecheckPrincipal(reqCtx)
		if !ok {
			return false
		}
		return len(m.filterAuthorizedTopics(fresh, topics)) == len(topics)
	}

	heartbeat := time.NewTicker(m.sseHeartbeat)
	defer heartbeat.Stop()
	bound := time.NewTimer(m.sseStreamBound)
	defer bound.Stop()

	for {
		select {
		case <-streamCtx.Done():
			return
		case <-bound.C:
			return
		case update := <-ch:
			if !held() {
				return
			}
			payload := ssePayload{
				Island: update.IslandID,
				HTML:   update.HTML,
			}
			data, err := json.Marshal(payload)
			if err != nil {
				sse.WriteError("json marshal error")
				continue
			}
			if err := sse.WriteEvent("island", string(data)); err != nil {
				return
			}
		case <-heartbeat.C:
			if !held() {
				return
			}
			if err := sse.WriteComment("ping"); err != nil {
				return
			}
		}
	}
}

// ConnectSession establishes one SSE subscriber on the session's stream for the client peer (its
// ClientKey), returning this connection's private update channel, its cancel, and an error when the
// concurrent-stream caps refuse the connect (see WithStreamCaps and WithPeerStreamCap).
func (m *Manager) ConnectSession(sessionID, peer string) (<-chan IslandUpdate, func(), error) {
	return m.subscribeImpl(sessionID, peer)
}

func (m *Manager) hintUntrustedProxy(r *http.Request) {
	ip := clientip.ParseHop(r.RemoteAddr)
	if !m.trustedFor(r).Empty() || ip == nil || !(ip.IsLoopback() || ip.IsPrivate()) {
		return
	}
	m.proxyHint.Do(func() {
		slog.Default().Warn("island: the per-client SSE stream cap refused a stream from a private address; behind a reverse proxy, list it in AppConfig.TrustedProxies or uihost.WithTrustedProxies or every visitor shares its budget",
			"peer", ip.String(), "cap", m.caps.perPeer)
	})
}

func (m *Manager) trustedFor(r *http.Request) clientip.Proxies {
	if m.proxiesSet {
		return m.proxies
	}
	return clientip.TrustedFrom(r.Context())
}

// ClientKey is the client a stream request counts under for the per-client cap: the signed-in user
// when identity names one, otherwise the caller's address.
func (m *Manager) ClientKey(r *http.Request, identity PresenceIdentity) string {
	if identity.UserID != "" {
		return "user:" + identity.UserID
	}
	ip := clientip.ParseHop(r.RemoteAddr)
	if proxies := m.trustedFor(r); ip != nil && proxies.Contains(ip) {
		if fwd, ok := clientip.Forwarded(r.Header, proxies); ok {
			ip = fwd
		}
	}
	switch {
	case ip == nil:
		return "peer:" + r.RemoteAddr
	case ip.To4() != nil:
		return "ip:" + ip.String() // a v4-mapped v6 address prints as v4
	default:
		return "ip:" + ip.Mask(net.CIDRMask(64, 128)).String()
	}
}
