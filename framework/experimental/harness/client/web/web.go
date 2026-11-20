//check-csp:ignore-file

package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/stream"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/inproc"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

const maxWebSendBody = 1 << 20

// Server is the bundled web client.
type Server struct {
	Client  *inproc.Client
	Session ids.SessionID

	seats *control.SeatTable

	listener net.Listener
	srv      *http.Server

	mu  sync.RWMutex
	bus *engine.Bus
}

// New constructs a Server that sends input through c and streams bus to every browser.
func New(c *inproc.Client, session ids.SessionID, bus *engine.Bus) *Server {
	return &Server{Client: c, Session: session, bus: bus, seats: control.DefaultSeatTable()}
}

func (s *Server) seatTable() *control.SeatTable {
	if s.seats != nil {
		return s.seats
	}
	return control.DefaultSeatTable()
}

// Start serves the web UI on a random 127.0.0.1 port and returns its URL.
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

// Stop waits for in-flight requests until ctx ends, then closes the connections left
// and returns ctx's error.
func (s *Server) Stop(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	err := s.srv.Shutdown(ctx)
	if err != nil {
		_ = s.srv.Close()
	}
	return err
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = fmt.Fprint(w, indexHTML)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	if guardLoopback(w, r) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	seat, seated := s.seatTable().AcquireSeat("", 0, stream.SeatOverflowRefuse)
	if !seated {
		w.Header().Set("Retry-After", "1")
		i18n.HTTPError(w, r, http.StatusTooManyRequests, i18n.ReasonTooManyStreams)
		return
	}
	defer s.seatTable().ReleaseSeat(seat)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	ch := s.bus.Subscribe(ctx)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	sse := stream.NewSSEWriter(w)
	for {
		select {
		case <-ctx.Done():
			return
		case <-seat.Done:
			return
		case env, ok := <-ch:
			if !ok {
				return
			}
			body, err := json.Marshal(env)
			if err != nil {
				continue
			}
			sse.SetID(strconv.FormatUint(env.ID, 10))
			if err := sse.WriteEvent(env.Kind, string(body)); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleInput(w http.ResponseWriter, r *http.Request) {
	if guardLoopback(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		i18n.HTTPError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
		return
	}
	defer r.Body.Close()
	var body struct {
		Text string `json:"text"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxWebSendBody)
	if err := handler.DecodeStrict(r.Body, &body); err != nil {
		i18n.HTTPError(w, r, http.StatusBadRequest, i18n.ReasonInvalidJSONBody)
		return
	}
	if body.Text == "" {
		i18n.HTTPError(w, r, http.StatusBadRequest, reasonEmptyText)
		return
	}
	if err := s.Client.Send(r.Context(), control.SendInput{
		SessionID: s.Session,
		Content:   engine.SimpleInput(body.Text),
	}); err != nil {
		writeJSONError(w, r, http.StatusConflict, reasonSendFailed, map[string]any{"detail": err.Error()})
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func writeJSONError(w http.ResponseWriter, r *http.Request, code int, reason i18n.Phrase, params ...map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"error":` + jsonString(reason.Text(r.Context(), params...)) + `,"reason":` + jsonString(reason.Code()) + `}`))
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

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
