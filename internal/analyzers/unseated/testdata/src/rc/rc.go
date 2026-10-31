// Package rc holds the unrechecked fixtures: long-lived streams that
// resolve a principal at connect, with and without a re-check on the
// delivery path, plus the silent postures. Names are unrelated to the
// repo's surfaces on purpose.
package rc

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"example.com/auth"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/stream"
)

func feed() <-chan string { return make(chan string, 1) }

// ---- firing: no re-check ---------------------------------------------

// signaler is the rtc shape (R18): a host Authorize func field run once
// on the request, then an upgrade and a read loop that keeps acting
// for the joined peer.
type signaler struct {
	Authorize func(r *http.Request) (string, error)
}

func (s *signaler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, err := s.Authorize(r); err != nil {
		http.Error(w, "no", http.StatusForbidden)
		return
	}
	s.serve(w, r)
}

func (s *signaler) serve(w http.ResponseWriter, r *http.Request) {
	conn, err := stream.Upgrade(w, r)
	if err != nil {
		return
	}
	for { // want `long-lived stream for a resolved principal delivers from this loop with no principal re-check`
		if _, err := conn.Read(); err != nil {
			return
		}
	}
}

// taskServer is the a2a shape (P17): the owner func field resolves in
// ServeHTTP, the open sits in a helper, the delivery loop in another.
type taskServer struct {
	owner func(*http.Request) (string, bool)
}

func (s *taskServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.owner(r)
	if !ok {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	s.subscribe(w, r, owner)
}

func (s *taskServer) subscribe(w http.ResponseWriter, r *http.Request, owner string) {
	open(w)
	s.forward(w, r.Context(), owner)
}

func open(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
}

func (s *taskServer) forward(w http.ResponseWriter, ctx context.Context, owner string) {
	for { // want `long-lived stream for a resolved principal delivers from this loop with no principal re-check`
		select {
		case <-ctx.Done():
			return
		case ev := <-feed():
			fmt.Fprintf(w, "data: %s %s\n\n", owner, ev)
		}
	}
}

// broker is the SSEBroker.Subscribe shape (P20): a Principal func field
// keys the stream, the open is core/stream's SSEWriter.
type broker struct {
	principal func(*http.Request) string
}

func (b *broker) Subscribe(w http.ResponseWriter, r *http.Request) {
	_ = b.principal(r)
	sse := stream.NewSSEWriter(w)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for { // want `long-lived stream for a resolved principal delivers from this loop with no principal re-check`
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			if sse.WriteComment("hb") != nil {
				return
			}
		}
	}
}

// userFeed reads the user off the context and streams with no re-check.
func userFeed(w http.ResponseWriter, r *http.Request) {
	if _, ok := handler.GetUser(r.Context()); !ok {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	for { // want `long-lived stream for a resolved principal delivers from this loop with no principal re-check`
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
	}
}

// ---- firing: identity re-checked, connect-time decision not re-run ----

// topicFeed is the presence shape (R8): the topic authorizer runs once
// at connect, RecheckPrincipal per push re-validates only the session.
func topicFeed(w http.ResponseWriter, r *http.Request, topics []string) {
	topics = authorizeTopics(r.Context(), topics) // want `connect-time decision authorizeTopics is never re-run`
	sse := stream.NewSSEWriter(w)
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			if _, ok := handler.RecheckPrincipal(r.Context()); !ok {
				return
			}
			_ = sse.WriteEvent("island", ev+fmt.Sprint(topics))
		}
	}
}

func authorizeTopics(ctx context.Context, topics []string) []string { return topics }

// tenantFeed is the CRUD event stream shape (D24): the tenant captured
// at connect filters every event; the re-check refreshes the user only.
func tenantFeed(w http.ResponseWriter, r *http.Request) {
	if _, ok := handler.GetUser(r.Context()); !ok {
		return
	}
	tenantID := handler.GetTenantID(r.Context()) // want `connect-time decision GetTenantID is never re-run`
	sse := stream.NewSSEWriter(w)
	held := func() bool {
		_, ok := handler.RecheckPrincipal(r.Context())
		return ok
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			if !held() {
				return
			}
			_ = sse.WriteEvent(tenantID, ev)
		}
	}
}

// ---- quiet: the fix spellings ----------------------------------------

// recheckPerDelivery is the crud / mcp GET spelling.
func recheckPerDelivery(w http.ResponseWriter, r *http.Request) {
	if _, ok := handler.GetUser(r.Context()); !ok {
		return
	}
	sse := stream.NewSSEWriter(w)
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			if _, ok := handler.RecheckPrincipal(r.Context()); !ok {
				return
			}
			_ = sse.WriteEvent("e", ev)
		}
	}
}

// recheckViaClosure re-checks through a closure the loop calls.
func recheckViaClosure(w http.ResponseWriter, r *http.Request) {
	if _, ok := handler.GetUser(r.Context()); !ok {
		return
	}
	sse := stream.NewSSEWriter(w)
	held := func() bool {
		_, ok := handler.RecheckPrincipal(r.Context())
		return ok
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			if !held() {
				return
			}
			_ = sse.WriteEvent("e", ev)
		}
	}
}

// tokenServer re-runs its resolver inside the loop (harness REST).
type tokenServer struct{}

func (s *tokenServer) verifyToken(r *http.Request) error { return nil }

func (s *tokenServer) events(w http.ResponseWriter, r *http.Request) {
	if s.verifyToken(r) != nil {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			if s.verifyToken(r) != nil {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
	}
}

// watchedSocket re-verifies in a sibling watch goroutine that closes
// the socket (harness ws revocationWatch).
type watchedSocket struct {
	token string
	conn  *stream.WebSocketConn
}

func serveWatched(w http.ResponseWriter, r *http.Request, tok string) {
	if auth.Verify(tok) != nil {
		return
	}
	conn, err := stream.Upgrade(w, r)
	if err != nil {
		return
	}
	c := &watchedSocket{token: tok, conn: conn}
	go c.watch(r.Context())
	c.pump()
}

func (c *watchedSocket) watch(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if auth.Verify(c.token) != nil {
				_ = c.conn.Close()
				return
			}
		}
	}
}

func (c *watchedSocket) pump() {
	for {
		select {
		case ev := <-feed():
			_ = c.conn.WriteString(ev)
		}
	}
}

// topicFeedFixed re-runs the topic authorizer per delivery.
func topicFeedFixed(w http.ResponseWriter, r *http.Request, topics []string) {
	topics = authorizeTopics(r.Context(), topics)
	sse := stream.NewSSEWriter(w)
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			live, ok := handler.RecheckPrincipal(r.Context())
			if !ok {
				return
			}
			topics = authorizeTopics(live, topics)
			_ = sse.WriteEvent("island", ev+fmt.Sprint(topics))
		}
	}
}

// tenantFeedFixed re-reads the tenant off the refreshed context.
func tenantFeedFixed(w http.ResponseWriter, r *http.Request) {
	if _, ok := handler.GetUser(r.Context()); !ok {
		return
	}
	tenantID := handler.GetTenantID(r.Context())
	sse := stream.NewSSEWriter(w)
	held := func() bool {
		fresh, ok := handler.RecheckPrincipal(r.Context())
		return ok && handler.GetTenantID(fresh) == tenantID
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			if !held() {
				return
			}
			_ = sse.WriteEvent(tenantID, ev)
		}
	}
}

// ---- quiet: silent postures ------------------------------------------

// anonymousFeed resolves no principal (kiln live, dev livereload): there
// is nothing to re-check.
func anonymousFeed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
	}
}

// oneShot resolves and opens but never parks.
func oneShot(w http.ResponseWriter, r *http.Request) {
	if _, ok := handler.GetUser(r.Context()); !ok {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprint(w, "data: done\n\n")
}

// silentPark resolves and opens, but its park loop delivers nothing to
// the client (no write, no conn read): not a delivery loop.
var parked int

func silentPark(w http.ResponseWriter, r *http.Request) {
	if _, ok := handler.GetUser(r.Context()); !ok {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	for {
		select {
		case <-r.Context().Done():
			return
		case <-feed():
			parked++
		}
	}
}

// labeled invokes a user-named func field without the request: a
// formatter, not a resolver, so the stream counts as anonymous.
type labeled struct {
	user func(name string) string
}

func (l *labeled) feed(w http.ResponseWriter, r *http.Request) {
	_ = l.user("x")
	w.Header().Set("Content-Type", "text/event-stream")
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-feed():
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
	}
}
