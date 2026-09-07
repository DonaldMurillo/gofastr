//go:build red

package main

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T3).
// CONTRACT-QUESTION: the remote peer is developer-configured via
// GOFASTR_URL (not attacker-chosen), so this is a dev-peer hygiene gap,
// not a hostile-input hole — but the repo's own convention caps peer
// responses (io.LimitReader before ReadAll/decode, 1 MiB in the generated
// clients and OAuth provider fetches), and these four reads predate it.
// Property: CLI remote semantic peer responses read through a byte cap
// before decode/error-print — a peer that dribbles a response body
// forever cannot pin unbounded heap in the CLI process.
// Surfaces: cmd/gofastr/semantic.go::remoteQuery :305 (error snapshot
// io.ReadAll), :311 (json.NewDecoder(resp.Body).Decode);
// ::remoteGet :324 (error snapshot io.ReadAll), :327 (io.ReadAll of the
// whole body). No cap on any of the four; both helpers use the default
// client (no timeout either, same family).
// Finding (probe shape): a remote peer answering /semantic/query with a
// paced never-ending body makes remoteQuery buffer the entire send stream
// before its decoder errors; remoteGet buffers it happily and returns.
// Fix direction: wrap every peer body in io.LimitReader(resp.Body, 1 MiB)
// (repo convention) before ReadAll/decode, erroring past the cap.

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/semantic"
)

// TestSemanticRemoteRedBodyCapped points the CLI's remote semantic helpers
// at a paced peer that serves more than the conventional 1 MiB cap and
// counts every byte delivered. Both helpers must stop pulling bytes once
// past the cap (error or truncation at the cap), not drain the peer's
// whole stream into heap.
func TestSemanticRemoteRedBodyCapped(t *testing.T) {
	const bodyTotal = 4 << 20 // peer sends 4 MiB; cap convention is 1 MiB
	const maxDelivered = 2 << 20

	t.Run("query", func(t *testing.T) {
		var delivered atomic.Int64
		srv := newSemanticRemoteRedPeer(t, bodyTotal, &delivered)
		defer srv.Close()

		_, err := remoteQuery(srv.URL, semantic.Query{Text: "x"})
		if d := delivered.Load(); d > maxDelivered {
			t.Errorf("SECURITY: [semantic-remote-unbounded] remoteQuery pulled %d bytes from the peer before returning (err=%v) — the response reads in semantic.go (remoteQuery :305/:311, remoteGet :324/:327) carry no byte cap; repo convention is io.LimitReader(resp.Body, 1 MiB) on peer responses so a dribbling peer errors at the cap instead of being buffered whole", d, err)
		}
	})

	t.Run("get", func(t *testing.T) {
		var delivered atomic.Int64
		srv := newSemanticRemoteRedPeer(t, bodyTotal, &delivered)
		defer srv.Close()

		_, _ = remoteGet(srv.URL + "/semantic/stats")
		if d := delivered.Load(); d > maxDelivered {
			t.Errorf("SECURITY: [semantic-remote-unbounded] remoteGet pulled %d bytes from the peer (err=nil expected: ReadAll succeeds) — the response reads in semantic.go (remoteQuery :305/:311, remoteGet :324/:327) carry no byte cap; repo convention is io.LimitReader(resp.Body, 1 MiB) on peer responses so a dribbling peer errors at the cap instead of being buffered whole", d)
		}
	})
}

// newSemanticRemoteRedPeer serves /semantic/query and /semantic/stats with
// a JSON value that never terminates (`{"hits":[` + spaces), dribbled in
// 64 KiB chunks so a capped consumer's stall is observable in the
// delivered counter. A write deadline bounds the handler against a client
// that stops reading.
func newSemanticRemoteRedPeer(t *testing.T, total int, delivered *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/semantic/query", "/semantic/stats":
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		chunk := make([]byte, 64<<10)
		for i := range chunk {
			chunk[i] = ' '
		}
		copy(chunk, `{"hits":[`) // legal JSON prefix: decoder keeps pulling
		sent := 0
		for sent < total {
			n, err := w.Write(chunk)
			delivered.Add(int64(n))
			sent += n
			if err != nil {
				return
			}
			time.Sleep(200 * time.Microsecond)
		}
	}))
	srv.Config.WriteTimeout = 5 * time.Second
	srv.Start()
	return srv
}
