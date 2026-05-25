//check-csp:ignore-file
// The bundled harness web client is a dev-only operator surface served
// on a random localhost port — not a production browser app — so the
// framework's strict-CSP contract does not apply. The indexHTML
// constant inlines the SSE listener + form handler so the harness
// runs without a build step or an extra HTTP round-trip.

// Package web implements the bundled web client.
//
// v0.1 minimum: serve a minimal HTML+JS shell on a random local port
// that talks to the engine via the same inproc transport the TUI
// uses. The page consumes Server-Sent Events from the harness bus
// and POSTs SendInput via a small JSON endpoint hosted by this same
// http.Server.
//
// Full GoFastr-App dogfooding (entities, island hydration, framework
// crud machinery) lands as the harness grows; v0.1 keeps the surface
// tight so the harness ships a usable browser UI today.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/inproc"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// Server is the bundled web client.
type Server struct {
	Client  *inproc.Client
	Session ids.SessionID

	listener net.Listener
	srv      *http.Server

	mu      sync.RWMutex
	bus     *engine.Bus // engine bus subscribed for SSE clients
}

// New constructs a Server bound to the given inproc Client + bus.
//
// The bus is needed (in addition to the Client) so each browser
// subscriber gets its own dedicated channel — the inproc.Client's
// Subscribe is one-channel-per-client, and we want N browsers to
// share one engine.
func New(c *inproc.Client, session ids.SessionID, bus *engine.Bus) *Server {
	return &Server{Client: c, Session: session, bus: bus}
}

// Start binds a TCP listener on 127.0.0.1:0 (random port) and serves
// the web UI. Returns the URL to print.
func (s *Server) Start() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	s.listener = ln
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/events", s.handleSSE)
	mux.HandleFunc("/input", s.handleInput)
	mux.HandleFunc("/health", s.handleHealth)
	s.srv = &http.Server{Handler: mux}
	go func() { _ = s.srv.Serve(ln) }()
	return "http://" + ln.Addr().String(), nil
}

// Stop terminates the server.
func (s *Server) Stop(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

// ---------- handlers ----------

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// X-Content-Type-Options + Referrer-Policy: defense in depth.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = fmt.Fprint(w, indexHTML)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flusher", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering if proxied
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	ch := s.bus.Subscribe(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case env, ok := <-ch:
			if !ok {
				return
			}
			body, err := json.Marshal(env)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", env.ID, env.Kind, body)
			flusher.Flush()
		}
	}
}

func (s *Server) handleInput(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad JSON", http.StatusBadRequest)
		return
	}
	if body.Text == "" {
		http.Error(w, "empty text", http.StatusBadRequest)
		return
	}
	if err := s.Client.Send(r.Context(), control.SendInput{
		SessionID: s.Session,
		Content:   engine.SimpleInput(body.Text),
	}); err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"error":` + jsonString(msg) + `}`))
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// indexHTML is the minimal SPA. v0.1 ships inline — when the
// GoFastr-App dogfooding lands this is replaced with island-rendered
// pages.
const indexHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>gofastr harness</title>
<style>
  body { font: 14px/1.4 system-ui, sans-serif; margin: 0; background: #0b0f17; color: #d6def0; }
  header { padding: .5rem 1rem; background: #11182a; border-bottom: 1px solid #1f2940; font-weight: 600; }
  #log { padding: 1rem; height: calc(100vh - 110px); overflow-y: auto; white-space: pre-wrap; }
  .line { margin-bottom: .25rem; }
  .user { color: #93c5fd; }
  .assistant { color: #d6def0; }
  .error { color: #fca5a5; }
  .meta  { color: #8aa0c8; font-size: 12px; }
  form { display: flex; gap: .5rem; padding: .5rem 1rem; background: #11182a; border-top: 1px solid #1f2940; }
  input[type=text] { flex: 1; padding: .5rem; background: #0b0f17; color: #d6def0; border: 1px solid #1f2940; border-radius: 4px; }
  button { padding: .5rem 1rem; background: #2563eb; color: white; border: 0; border-radius: 4px; cursor: pointer; }
</style>
</head>
<body>
  <header>gofastr harness</header>
  <div id="log"></div>
  <form id="form">
    <input id="input" type="text" autocomplete="off" autofocus placeholder="Type and press Enter…" />
    <button type="submit">Send</button>
  </form>
<script>
const log = document.getElementById('log');
const form = document.getElementById('form');
const input = document.getElementById('input');

function append(cls, text) {
  const div = document.createElement('div');
  div.className = 'line ' + cls;
  div.textContent = text;
  log.appendChild(div);
  log.scrollTop = log.scrollHeight;
}

const es = new EventSource('/events');
es.addEventListener('TextDelta', (ev) => {
  const env = JSON.parse(ev.data);
  let last = log.lastElementChild;
  if (!last || !last.classList.contains('assistant') || last.dataset.done) {
    last = document.createElement('div');
    last.className = 'line assistant';
    log.appendChild(last);
  }
  last.textContent += JSON.parse(env.payload).text;
  log.scrollTop = log.scrollHeight;
});
es.addEventListener('TurnEnded', () => {
  const last = log.lastElementChild;
  if (last && last.classList.contains('assistant')) last.dataset.done = '1';
});
es.addEventListener('Error', (ev) => {
  const env = JSON.parse(ev.data);
  append('error', '[error] ' + JSON.parse(env.payload).message);
});
es.addEventListener('PermissionRequested', (ev) => {
  const env = JSON.parse(ev.data);
  const p = JSON.parse(env.payload);
  append('meta', '[permission] ' + p.tool + ' (use the TUI or POST to /v1/sessions/.../permission to answer)');
});

form.addEventListener('submit', async (e) => {
  e.preventDefault();
  const text = input.value;
  if (!text) return;
  input.value = '';
  append('user', '> ' + text);
  await fetch('/input', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({text}),
  });
});
</script>
</body>
</html>
`
