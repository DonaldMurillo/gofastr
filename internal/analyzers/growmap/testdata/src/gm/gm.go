// Package gm is core/mcp's subscription record before d3cfff016:
// resourceSubsBy is keyed by the caller (the peer host when anonymous)
// and nothing caps it or expires it.
package gm

import (
	"context"
	"net/http"
	"sync"
	"time"
)

type resourceSubKey struct{ caller, uri string }

type ctxKey struct{}

func subscriptionCaller(ctx context.Context) string {
	if s, ok := ctx.Value(ctxKey{}).(string); ok {
		return s
	}
	return ""
}

const maxResourceSubscriptions = 1024

type Server struct {
	mu             sync.Mutex
	resourceSubs   map[string]int
	resourceSubsBy map[resourceSubKey]int
	lastSeen       map[string]time.Time
	stamps         map[string]int64
	stale          map[string]bool
	hits           map[string]int
	sessions       map[string][]string
	tenants        map[string]bool
}

type subscribeParams struct{ URI string }

func (s *Server) handleResourcesSubscribe(ctx context.Context, params subscribeParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The shared per-uri count is capped: quiet.
	if _, ok := s.resourceSubs[params.URI]; !ok && len(s.resourceSubs) >= maxResourceSubscriptions {
		return nil
	}
	s.resourceSubs[params.URI]++
	s.resourceSubsBy[resourceSubKey{caller: subscriptionCaller(ctx), uri: params.URI}]++ // want `resourceSubsBy grows by a key derived from the caller`
	return nil
}

// The *http.Request spelling: the peer address straight from the request,
// through a local.
func (s *Server) touch(w http.ResponseWriter, r *http.Request) {
	host := r.RemoteAddr
	s.mu.Lock()
	s.hits[host] += 1 // want `hits grows by a key derived from the caller`
	s.mu.Unlock()
}

// append into the entry, inside a closure on the dispatch path.
func (s *Server) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Session")
		s.sessions[id] = append(s.sessions[id], r.URL.Path) // want `sessions grows by a key derived from the caller`
	}
}

// Quiet: lastSeen is expired by a ticker-driven sweep.
func (s *Server) seen(ctx context.Context) {
	s.lastSeen[subscriptionCaller(ctx)] = time.Now()
}

func (s *Server) sweepLoop(stop <-chan struct{}) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			s.mu.Lock()
			for k, v := range s.lastSeen {
				if now.Sub(v) > time.Hour {
					delete(s.lastSeen, k)
				}
			}
			s.mu.Unlock()
		}
	}
}

// The sweep whose TTL tests read Unix seconds and time.Time.Compare,
// with the deletes AFTER the select's tick case instead of inside it:
// the loop that receives from the ticker drives both.
func (s *Server) sweepUnix(stop <-chan struct{}) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		s.mu.Lock()
		now := time.Now()
		for k := range s.stale {
			delete(s.stale, k)
		}
		for k, v := range s.stamps {
			if now.Unix()-v > 600 {
				delete(s.stamps, k)
			}
		}
		for k, v := range s.lastSeen {
			if v.Compare(now.Add(-10*time.Minute)) < 0 {
				delete(s.lastSeen, k)
			}
		}
		s.mu.Unlock()
	}
}

// stampUnix records the caller under the sweeps above: both maps are
// bounded by the ticker loop, quiet.
func (s *Server) stampUnix(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.stamps[r.RemoteAddr] = time.Now().Unix()
	s.stale[r.RemoteAddr] = true
	s.lastSeen[r.RemoteAddr] = time.Now()
}

// Near miss: a caller-driven delete (unsubscribe) bounds nothing; the
// attacker never sends it. Quiet only because the key below is from the
// body, not the caller.
func (s *Server) unsubscribe(ctx context.Context, params subscribeParams) {
	delete(s.resourceSubsBy, resourceSubKey{caller: subscriptionCaller(ctx), uri: params.URI})
	s.tenants[params.URI] = true
}

// Near miss: a per-request accumulator on a local value dies with the
// request.
type acc struct{ seen map[string]bool }

func collect(ctx context.Context) {
	var a acc
	a.seen = map[string]bool{}
	a.seen[subscriptionCaller(ctx)] = true
}

// Near miss: a write with no identity root in scope.
func (s *Server) record(caller string) {
	s.hits[caller]++
}

// An emptiness check is no cap: len(pokes) == 0 only asks whether the
// record is empty, not whether it is bounded.
type poked struct {
	pokes map[string]int
}

func (p *poked) poke(ctx context.Context) {
	p.pokes[subscriptionCaller(ctx)]++ // want `pokes grows by a key derived from the caller`
}

func (p *poked) empty() bool { return len(p.pokes) == 0 }

// Near miss for the sweep credit: a caller-driven unsubscribe that
// stamps the clock. The delete is driven by the caller (the attacker
// never sends it), so it sweeps nothing.
type stamped struct {
	subs      map[string]bool
	lastLeave time.Time
}

func (s *stamped) join(ctx context.Context) {
	s.subs[subscriptionCaller(ctx)] = true // want `subs grows by a key derived from the caller`
}

func (s *stamped) leave(ctx context.Context) {
	delete(s.subs, subscriptionCaller(ctx))
	s.lastLeave = time.Now()
}

// The marker for a map bounded by construction.
func (s *Server) mark(ctx context.Context) {
	//gofastr:allow(growmap) keys come from the closed tenant registry the server mints
	s.tenants[subscriptionCaller(ctx)] = true
}

// The pre-d3cfff016 near miss a sloppy matcher takes for a sweep: an
// expire-named clear that runs only when the last stream closes, with
// no clock in it. Subscribe needs no stream, so it never runs.
type notifier struct {
	streams map[int]bool
	subsBy  map[resourceSubKey]int
}

func (n *notifier) expireResourceSubsIfIdleLocked() {
	if len(n.streams) == 0 {
		clear(n.subsBy)
	}
}

func (n *notifier) subscribe(ctx context.Context, uri string) {
	n.subsBy[resourceSubKey{caller: subscriptionCaller(ctx), uri: uri}]++ // want `subsBy grows by a key derived from the caller`
}

// Quiet: the entry is released by a defer when the stream ends
// (core/stream's seatOrder), so it is bounded by live connections.
type broker struct{ seats map[string][]int }

func (b *broker) release(p string) { delete(b.seats, p) }

func (b *broker) Subscribe(w http.ResponseWriter, r *http.Request) {
	p := r.Header.Get("X-Principal")
	b.seats[p] = append(b.seats[p], 1)
	defer b.release(p)
	<-r.Context().Done()
}
