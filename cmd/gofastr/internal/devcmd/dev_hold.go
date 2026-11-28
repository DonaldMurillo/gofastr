package devcmd

import (
	"fmt"
	"html"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/loopback"
	"github.com/DonaldMurillo/gofastr/core/config"
	"github.com/DonaldMurillo/gofastr/core/mediatype"
	"github.com/DonaldMurillo/gofastr/framework/dev"
)

const (
	holdBuilding    = "building"
	holdBuildFailed = "build-failed"
	holdExited      = "exited"
)

const holdDetailLimit = 64 << 10

const holdRelistenFor = 3 * time.Second

// devHold answers the dev address while no app process serves it.
type devHold struct {
	addr       string
	idPrefix   string
	exposed    bool
	liveReload bool
	hosts      []string

	mu      sync.Mutex
	ln      net.Listener
	srv     *http.Server
	phase   string
	detail  string
	gen     int
	changed chan struct{}

	listenMu sync.Mutex
}

func newDevHold(addr string) (*devHold, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	exposed := !boundLoopback(ln.Addr())
	hosts, _ := loopback.LoopbackGuards(ln.Addr().String())
	h := &devHold{
		hosts:      hosts,
		addr:       ln.Addr().String(),
		idPrefix:   fmt.Sprintf("gofastr-dev-hold-%d-", time.Now().UnixNano()),
		exposed:    exposed,
		liveReload: !exposed || config.EnvBool(dev.LiveReloadExposeEnv),
		phase:      holdBuilding,
		changed:    make(chan struct{}),
	}
	h.mu.Lock()
	h.serveLocked(ln)
	h.mu.Unlock()
	return h, nil
}

func boundLoopback(a net.Addr) bool {
	tcp, ok := a.(*net.TCPAddr)
	return ok && tcp.IP.IsLoopback()
}

func (h *devHold) serveLocked(ln net.Listener) {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	h.ln, h.srv = ln, srv
	go func() { _ = srv.Serve(ln) }()
}

func (h *devHold) show(phase, detail string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.phase, h.detail = phase, tailString(stripANSI(detail), holdDetailLimit)
	h.gen++
	close(h.changed)
	h.changed = make(chan struct{})
	h.mu.Unlock()
	h.relisten()
}

func (h *devHold) relisten() {
	h.listenMu.Lock()
	defer h.listenMu.Unlock()
	h.mu.Lock()
	serving := h.srv != nil
	h.mu.Unlock()
	if serving {
		return
	}
	deadline := time.Now().Add(holdRelistenFor)
	for {
		ln, err := net.Listen("tcp", h.addr)
		if err == nil {
			h.mu.Lock()
			h.serveLocked(ln)
			h.mu.Unlock()
			return
		}
		if time.Now().After(deadline) {
			cli.Warn("Could not hold %s while the app is down: %v", h.addr, err)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (h *devHold) release() {
	if h == nil {
		return
	}
	h.listenMu.Lock()
	defer h.listenMu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.srv != nil {
		_ = h.ln.Close()
		_ = h.srv.Close()
		h.ln, h.srv = nil, nil
	}
}

func (h *devHold) state() (phase, detail string, gen int, changed chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.phase, h.detail, h.gen, h.changed
}

func (h *devHold) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/__livereload":
		if !h.liveReload {
			http.NotFound(w, r)
			return
		}
		if !h.exposed && !loopback.HostAllowed(r.Host, h.hosts) {
			http.Error(w, "gofastr dev: this address answers only its own host names", http.StatusForbidden)
			return
		}
		h.serveLiveReload(w, r)
		return
	case holdScriptURL:
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = fmt.Fprint(w, holdReloadJS)
		return
	}
	phase, detail, gen, _ := h.state()
	withheld := h.exposed || !loopback.HostAllowed(r.Host, h.hosts)
	if withheld {
		detail = ""
	}
	title, lead := holdMessage(phase, withheld, h.exposed)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Retry-After", "1")
	w.Header().Set("X-Gofastr-Dev", phase)
	var body string
	if strings.Contains(mediatype.Fold(r.Header.Get("Accept")), "text/html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		body = holdPage(title, lead, detail, h.idPrefix+fmt.Sprint(gen))
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		body = "gofastr dev: " + lead + "\n"
		if detail != "" {
			body += "\n" + detail
		}
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = fmt.Fprint(w, body)
}

func (h *devHold) serveLiveReload(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	//gofastr:allow(unseated) dev-only stream on the developer's own address, served only while no app is up; release closes every one
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	sent := -1
	for {
		phase, _, gen, changed := h.state()
		if phase != holdBuilding && gen != sent {
			//gofastr:allow(nowritedeadline) release closes the server (h.srv.Close), which ends a blocked write
			_, _ = fmt.Fprintf(w, "event: ready\ndata: %s%d\n\n", h.idPrefix, gen)
			flusher.Flush()
			sent = gen
		}
		select {
		case <-changed:
		case <-r.Context().Done():
			return
		}
	}
}

func holdMessage(phase string, withheld, exposed bool) (title, lead string) {
	switch phase {
	case holdBuildFailed:
		where := "below"
		switch {
		case exposed:
			where = "in the terminal running gofastr dev (this address is reachable from other machines, so the page leaves the build output out)"
		case withheld:
			where = "in the terminal running gofastr dev (the request named a host other than this machine, so the page leaves the build output out)"
		}
		return "Build failed", "the last build failed. Fix the error " + where + " and save; " +
			"gofastr dev rebuilds on every save. It is still running, so do not start another one."
	case holdExited:
		return "The app stopped", "the app process exited. Its output is in the terminal " +
			"running gofastr dev. Save a file to rebuild and restart it; gofastr dev is still running."
	default:
		return "Building", "building the app. This address serves the app once the build " +
			"finishes, so retry in a few seconds. gofastr dev is already running; do not start another one."
	}
}

const holdScriptURL = "/__gofastr/dev-hold.js"

const holdReloadJS = `(() => {
  const id = document.currentScript.dataset.holdId;
  const es = new EventSource('/__livereload');
  es.addEventListener('ready', (e) => {
    if (e.data !== id) location.reload();
  });
})();`

func holdPage(title, lead, detail, id string) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	b.WriteString(`<meta name="color-scheme" content="light dark">`)
	fmt.Fprintf(&b, `<title>%s · gofastr dev</title></head><body><main>`, html.EscapeString(title))
	fmt.Fprintf(&b, `<h1>%s</h1><p>gofastr dev: %s</p>`, html.EscapeString(title), html.EscapeString(lead))
	if detail != "" {
		fmt.Fprintf(&b, `<pre>%s</pre>`, html.EscapeString(detail))
	}
	fmt.Fprintf(&b, `</main><script src="%s" data-hold-id="%s"></script>`, holdScriptURL, html.EscapeString(id))
	b.WriteString(`</body></html>`)
	return b.String()
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func stripANSI(s string) string {
	for {
		next := ansiEscape.ReplaceAllString(s, "")
		if next == s {
			return s
		}
		s = next
	}
}

func tailString(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return "…" + strings.ToValidUTF8(s[len(s)-limit:], "")
}

type holdOutput struct {
	mu sync.Mutex
	b  []byte
}

func (o *holdOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.b = append(o.b, p...)
	if len(o.b) > 2*holdDetailLimit {
		o.b = append([]byte(nil), o.b[len(o.b)-holdDetailLimit:]...)
	}
	return len(p), nil
}

func (o *holdOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return string(o.b)
}
