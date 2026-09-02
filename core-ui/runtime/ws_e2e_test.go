package runtime

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// --- raw WebSocket test server ---------------------------------------
//
// The e2e tests here need to deliver frames the framework's own
// WebSocketConn cannot send on demand (a close frame carrying a
// planted secret reason, an abrupt TCP kill with no close frame), so
// the server side is a hand-rolled handshake plus raw frames.

func wsAcceptKey(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// wsHandshake completes an RFC 6455 upgrade and returns the raw conn.
func wsHandshake(w http.ResponseWriter, r *http.Request) (net.Conn, error) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, fmt.Errorf("response writer cannot hijack")
	}
	conn, bufrw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	if bufrw != nil {
		_ = bufrw.Writer.Flush()
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + wsAcceptKey(r.Header.Get("Sec-WebSocket-Key")) + "\r\n\r\n"
	if _, err := conn.Write([]byte(resp)); err != nil {
		return nil, err
	}
	return conn, nil
}

// wsFrame builds one unmasked server-to-client frame.
func wsFrame(opcode byte, payload []byte) []byte {
	f := []byte{opcode, byte(len(payload))}
	if len(payload) >= 126 {
		f = []byte{opcode, 126, byte(len(payload) >> 8), byte(len(payload) & 0xff)}
	}
	return append(f, payload...)
}

func wsText(payload string) []byte { return wsFrame(0x81, []byte(payload)) }

// wsCloseReason builds a close frame carrying a status code and a
// UTF-8 reason.
func wsCloseReason(code uint16, reason string) []byte {
	payload := make([]byte, 2+len(reason))
	payload[0] = byte(code >> 8)
	payload[1] = byte(code)
	copy(payload[2:], reason)
	return wsFrame(0x88, payload)
}

// wsTestPage serves the runtime plus a script body. The script must
// loadModule('ws') itself before using the module API.
func wsTestPage(t *testing.T, mux *http.ServeMux, script string) string {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	if mod, ok := Module("ws"); ok {
		mux.HandleFunc("/__gofastr/runtime/ws.js", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = w.Write([]byte(mod))
		})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head></head><body><span id="ready">ready</span>`+
			`<script src="/__gofastr/runtime.js"></script><script>`+script+`</script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// wsConsoleTap must be prepended to the page script BEFORE the module
// loads: it captures every console call so a test can assert a planted
// secret never reached the log.
const wsConsoleTap = `
(() => {
  window.__logs = [];
  for (const level of ['log', 'info', 'warn', 'error', 'debug']) {
    const orig = console[level].bind(console);
    console[level] = (...a) => {
      window.__logs.push(level + ': ' + a.map((x) => {
        try { return typeof x === 'string' ? x : JSON.stringify(x); } catch (_) { return '?'; }
      }).join(' '));
      orig(...a);
    };
  }
})();
`

// The Field Assist ordering regression (#375), through the shipped
// reducer in a real browser: a snapshot captured before a live clear
// event arrives after it, and the cleared state must stay cleared.
// Applied in this order: snapshot 41 (banner live), event 42 (clear),
// the delayed snapshot 41 again, a duplicated event 42.
func TestWSReducerRejectsStaleSnapshot(t *testing.T) {
	mux := http.NewServeMux()
	script := wsConsoleTap + `
    __gofastr.loadModule('ws').then(() => {
      const reduce = __gofastr.createSequencedReducer(
        { banner: null },
        (state, env) => ({ banner: env.payload.banner })
      );
      const r1 = reduce({ sequence: 41, type: 'snapshot', payload: { banner: 'HOLD STEADY' } });
      const r2 = reduce({ sequence: 42, type: 'cleared', payload: { banner: null } });
      const r3 = reduce({ sequence: 41, type: 'snapshot', payload: { banner: 'HOLD STEADY' } });
      const r4 = reduce({ sequence: 42, type: 'cleared', payload: { banner: null } });
      window.__res = [r1, r2, r3, r4];
      window.__done = true;
    });
  `
	base := wsTestPage(t, mux, script)
	ctx := newSeedBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`window.__done === true`, nil, chromedp.WithPollingTimeout(10*time.Second)),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify(window.__res)`, &raw)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	var res []struct {
		Applied         bool   `json:"applied"`
		AppliedSequence uint64 `json:"appliedSequence"`
		State           struct {
			Banner *string `json:"banner"`
		} `json:"state"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("decode probe: %v", err)
	}
	if len(res) != 4 {
		t.Fatalf("got %d reducer results, want 4", len(res))
	}
	wantApplied := []bool{true, true, false, false}
	for i, want := range wantApplied {
		if res[i].Applied != want {
			t.Fatalf("envelope %d applied = %v, want %v (the guard must reject sequence <= applied)", i, res[i].Applied, want)
		}
	}
	if res[3].State.Banner != nil {
		t.Fatalf("banner after stale snapshot = %q, want null (cleared state stays cleared)", *res[3].State.Banner)
	}
	if res[3].AppliedSequence != 42 {
		t.Fatalf("appliedSequence = %d, want 42", res[3].AppliedSequence)
	}
}

// wsProbe is the page-side result shape shared by the socket tests.
type wsProbe struct {
	Seen     []string `json:"seen"`
	Protocol struct {
		DurableSession string   `json:"durableSession"`
		OfferPending   bool     `json:"offerPending"`
		PeerConnected  string   `json:"peerConnected"`
		PeerFailed     string   `json:"peerFailed"`
		AcceptedReady  []string `json:"acceptedReady"`
	} `json:"protocol"`
	Status struct {
		Generation   uint64 `json:"generation"`
		Phase        string `json:"phase"`
		ReasonClass  string `json:"reasonClass"`
		LastSequence uint64 `json:"lastSequence"`
	} `json:"status"`
	Events    string `json:"events"`
	Logs      string `json:"logs"`
	RawReason string `json:"rawReason"`
}

// The #377 minimal failure, replayed and fixed: generation 1 carries a
// ready pulse and an in-flight negotiation when the transport dies
// without a close frame; generation 2 reconnects, hydrates, and a
// fresh operator-ready pulse with the SAME id must be accepted, not
// discarded as a duplicate. A healthy peer survives the reconnect; a
// failed peer is replaced; generation ids are distinct.
func TestWSReconnectFreshEventNotDiscarded(t *testing.T) {
	var conns atomic.Int32
	hold := make(chan struct{})
	t.Cleanup(func() { close(hold) })

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsHandshake(w, r)
		if err != nil {
			return
		}
		switch conns.Add(1) {
		case 1:
			_, _ = conn.Write(wsText(`{"type":"operator-ready","sequence":5,"payload":{"id":"pulse-1"}}`))
			_ = conn.Close() // abrupt: no close frame → abnormal close
		default:
			_, _ = conn.Write(wsText(`{"type":"snapshot","sequence":10,"payload":{"banner":null}}`))
			_, _ = conn.Write(wsText(`{"type":"operator-ready","sequence":11,"payload":{"id":"pulse-1"}}`))
			<-hold // keep generation 2 open for the rest of the test
		}
	})

	script := wsConsoleTap + `
    __gofastr.loadModule('ws').then(() => {
      const protocol = {
        durableSession: 'session-9',   // durable state: survives every reconnect
        offerPending: false,           // generation-bound work
        peerConnected: 'peer-A',       // healthy peer: preserved across reconnect
        peerFailed: 'peer-B',          // failed peer: replaced
        acceptedReady: [],
        seenReady: {},
      };
      const seen = [];
      const events = [];
      const ws = __gofastr.connectWebSocket('ws://' + location.host + '/ws', {
        onGenerationStart: (info) => {
          seen.push('start:' + info.generation + ':' + info.resumedAfterSequence);
          events.push({ hook: 'start', info });
          // A new generation invalidates only generation-bound work:
          protocol.offerPending = false;                          // stale offer dead with its transport
          protocol.peerFailed = 'peer-replaced-' + info.generation; // replace non-connected peers
          // protocol.peerConnected deliberately untouched: a healthy
          // protocol survives a transient reconnect without teardown.
        },
        onHydrated: (info) => {
          seen.push('hydrated:' + info.generation + ':' + info.snapshotSequence);
          events.push({ hook: 'hydrated', info });
          ws.resyncComplete();
        },
        onGenerationEnd: (info) => {
          seen.push('end:' + info.generation + ':' + info.reasonClass);
          events.push({ hook: 'end', info });
        },
        onMessage: (m) => {
          if (!m.data) return;
          // The duplicate hydrated() call is deliberate: hooks must be
          // idempotent per generation, so the second call is a no-op.
          if (m.data.type === 'snapshot') { ws.hydrated(m.data.sequence); ws.hydrated(m.data.sequence); }
          if (m.data.type === 'operator-ready') {
            // Dedup is keyed per generation: a pulse an earlier
            // generation already saw is NOT a duplicate in this one.
            const key = ws.status.generation + ':' + m.data.payload.id;
            if (!protocol.seenReady[key]) {
              protocol.seenReady[key] = true;
              protocol.acceptedReady.push(m.data.payload.id);
              // The fresh authoritative pulse completes negotiation;
              // generation 1's pulse reopens it (support calls again).
              protocol.offerPending = ws.status.generation === 1;
            }
          }
        },
      });
      window.__probe = () => ({
        seen,
        protocol,
        status: ws.status,
        events: JSON.stringify(events),
        logs: JSON.stringify(window.__logs),
      });
      window.__ready = true;
    });
  `
	base := wsTestPage(t, mux, script)
	ctx := newSeedBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`window.__ready === true`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		// Generation 1 gets its pulse and dies; the module reconnects
		// (first retry ~1s) and generation 2 delivers snapshot + pulse.
		chromedp.Poll(`window.__probe && window.__probe().protocol.acceptedReady.length === 2 && window.__probe().status.phase === 'resynced'`,
			nil, chromedp.WithPollingTimeout(20*time.Second), chromedp.WithPollingInterval(250*time.Millisecond)),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify(window.__probe())`, &raw)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	var p wsProbe
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("decode probe: %v", err)
	}

	if len(p.Protocol.AcceptedReady) != 2 || p.Protocol.AcceptedReady[0] != "pulse-1" || p.Protocol.AcceptedReady[1] != "pulse-1" {
		t.Fatalf("accepted ready pulses = %v, want [pulse-1 pulse-1] (the fresh generation-2 pulse must not be discarded)", p.Protocol.AcceptedReady)
	}
	if p.Status.Generation != 2 {
		t.Fatalf("final generation = %d, want 2 (distinct ids per reconnect)", p.Status.Generation)
	}
	if p.Protocol.OfferPending {
		t.Fatal("offerPending still true after reconnect: generation-bound negotiation was not invalidated")
	}
	if p.Protocol.PeerConnected != "peer-A" {
		t.Fatalf("healthy peer = %q, want peer-A (a healthy protocol must survive a transient reconnect)", p.Protocol.PeerConnected)
	}
	if p.Protocol.PeerFailed == "peer-B" || p.Protocol.PeerFailed == "" {
		t.Fatalf("failed peer = %q, want a replacement", p.Protocol.PeerFailed)
	}
	if p.Protocol.DurableSession != "session-9" {
		t.Fatalf("durable session = %q, want session-9", p.Protocol.DurableSession)
	}
	if p.Status.Phase != "resynced" {
		t.Fatalf("phase = %q, want resynced", p.Status.Phase)
	}
	if p.Status.LastSequence != 11 {
		t.Fatalf("lastSequence = %d, want 11", p.Status.LastSequence)
	}

	// Hook idempotency: the duplicate hydrated() call in the snapshot
	// path must not fire onHydrated twice for generation 2.
	if n := strings.Count(p.Events, `"hook":"hydrated"`); n != 1 {
		t.Fatalf("onHydrated fired %d times, want 1 (hooks are idempotent per generation)", n)
	}
	// Hook coverage: socket open, snapshot hydration, and generation
	// end are distinct events, in order, with the abrupt kill
	// classified.
	joined := strings.Join(p.Seen, " ")
	for _, want := range []string{"start:1:0", "hydrated:2:10", "end:1:error", "start:2:5"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("lifecycle missing %q in %v", want, p.Seen)
		}
	}
}

// Close reasons never reach the log or any observable surface: the
// server closes with a close frame whose REASON carries a planted
// secret, and only a bounded reason class may survive.
func TestWSCloseReasonNeverLogged(t *testing.T) {
	const secret = "hunter2-close-secret"
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsHandshake(w, r)
		if err != nil {
			return
		}
		_, _ = conn.Write(wsCloseReason(1011, "token="+secret))
		_ = conn.Close()
	})

	script := wsConsoleTap + `
    __gofastr.loadModule('ws').then(() => {
      const events = [];
      const ws = __gofastr.connectWebSocket('ws://' + location.host + '/ws', {
        reconnect: false,
        onGenerationStart: (info) => events.push({ hook: 'start', info }),
        onHydrated: (info) => events.push({ hook: 'hydrated', info }),
        onGenerationEnd: (info) => events.push({ hook: 'end', info }),
        onMessage: (m) => events.push({ hook: 'message', m }),
      });
      // An independent raw socket records the close event verbatim:
      // it proves the planted reason actually reaches the page, so the
      // leak assertions below are about real bytes, not a broken wire.
      const raw = new WebSocket('ws://' + location.host + '/ws');
      window.__rawReason = '';
      raw.onclose = (ev) => { window.__rawReason = ev.reason || '(none)'; };
      window.__probe = () => ({
        seen: [],
        protocol: {},
        status: ws.status,
        events: JSON.stringify(events),
        logs: JSON.stringify(window.__logs),
        rawReason: window.__rawReason,
      });
      window.__ready = true;
    });
  `
	base := wsTestPage(t, mux, script)
	ctx := newSeedBrowserCtx(t)

	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`window.__ready === true`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Poll(`window.__probe && window.__probe().status.phase === 'closed' && window.__probe().rawReason !== ''`,
			nil, chromedp.WithPollingTimeout(10*time.Second), chromedp.WithPollingInterval(200*time.Millisecond)),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify(window.__probe())`, &raw)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	var p wsProbe
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("decode probe: %v", err)
	}

	if !strings.Contains(p.RawReason, secret) {
		t.Fatalf("control socket saw close reason %q — the secret never reached the page, the leak assertions are vacuous", p.RawReason)
	}
	if p.Status.ReasonClass != "closed" && p.Status.ReasonClass != "error" {
		t.Fatalf("reasonClass = %q, want a bounded class (closed|error)", p.Status.ReasonClass)
	}
	statusJSON, err := json.Marshal(p.Status)
	if err != nil {
		t.Fatalf("re-encode status: %v", err)
	}
	for name, surface := range map[string]string{
		"console log":   p.Logs,
		"hook payloads": p.Events,
		"status object": string(statusJSON),
	} {
		if strings.Contains(surface, secret) {
			t.Fatalf("planted close-reason secret leaked via the %s", name)
		}
	}
}
