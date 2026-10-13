package main

import (
	"fmt"
	"html"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// What the hold says about the app while it holds the dev address.
const (
	holdBuilding    = "building"
	holdBuildFailed = "build-failed"
	holdExited      = "exited"
)

// holdDetailLimit caps the build output a hold page carries. A failing
// build that prints more is cut from the front: the last lines name the
// error the developer fixes next.
const holdDetailLimit = 64 << 10

// devHold answers the dev address while no app process is serving it:
// during the first build, after a failed build, and after the app exits.
// Without it a request in those windows gets "connection refused", which
// reads as a dead server, and agents relaunched `gofastr dev` on top of
// the one still running (issue #483). Every request gets a 503 with
// Retry-After that says which of the three it is, the build or lint
// output included.
//
// The hold never proxies. It closes its listener right before the app
// starts, so the app binds the address itself and runs exactly as it
// does without gofastr dev. Between that close and the app's own bind
// the address refuses connections for as long as the app takes to boot.
//
// It also answers /__livereload, the endpoint an open app tab's reload
// client reconnects to when the app goes away. It sends a ready event
// only when there is an error to show: a tab sitting through an
// ordinary rebuild never reloads into a "building" page, and when the
// app comes back its new build ID reloads the tab as before.
type devHold struct {
	addr     string // the bound address, reused on every relisten
	idPrefix string

	mu      sync.Mutex
	ln      net.Listener
	srv     *http.Server
	phase   string
	detail  string
	gen     int
	changed chan struct{} // closed and replaced on every show

	listenMu sync.Mutex // one relisten at a time
}

// newDevHold binds addr now, so a busy address fails `gofastr dev` at
// startup instead of after the first build.
func newDevHold(addr string) (*devHold, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	h := &devHold{
		addr:     ln.Addr().String(),
		idPrefix: fmt.Sprintf("gofastr-dev-hold-%d-", time.Now().UnixNano()),
		phase:    holdBuilding,
		changed:  make(chan struct{}),
	}
	h.mu.Lock()
	h.serveLocked(ln)
	h.mu.Unlock()
	return h, nil
}

func (h *devHold) serveLocked(ln net.Listener) {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	h.ln, h.srv = ln, srv
	go func() { _ = srv.Serve(ln) }()
}

// show records what the hold says and takes the address back if the
// hold had released it. A nil hold does nothing, so callers without a
// dev address (tests on :0) skip it.
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

// relisten rebinds the address after a release. The app that held it
// was just killed, and the kernel frees its socket when the process is
// gone, so the first tries can still find it bound.
func (h *devHold) relisten() {
	h.listenMu.Lock()
	defer h.listenMu.Unlock()
	h.mu.Lock()
	serving := h.srv != nil
	h.mu.Unlock()
	if serving {
		return
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		ln, err := net.Listen("tcp", h.addr)
		if err == nil {
			h.mu.Lock()
			h.serveLocked(ln)
			h.mu.Unlock()
			return
		}
		if time.Now().After(deadline) {
			warn("Could not hold %s while the app is down: %v", h.addr, err)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// release closes the listener and every open connection, livereload
// streams included, so the app can bind the address and the open tabs
// reconnect to it. It waits out a relisten still retrying, which would
// otherwise bind the address after the release and keep the app off it.
func (h *devHold) release() {
	if h == nil {
		return
	}
	h.listenMu.Lock()
	defer h.listenMu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.srv != nil {
		// Close the listener directly: Serve may not have registered it
		// yet, and then srv.Close alone would leave the port bound.
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
		h.liveReload(w, r)
		return
	case holdScriptURL:
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = fmt.Fprint(w, holdReloadJS)
		return
	}
	phase, detail, gen, _ := h.state()
	title, lead := holdMessage(phase)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Retry-After", "1")
	w.Header().Set("X-Gofastr-Dev", phase)
	var body string
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
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
	if r.Method != http.MethodHead {
		_, _ = fmt.Fprint(w, body)
	}
}

// liveReload speaks the framework/dev livereload protocol: a ready
// event carrying a build ID, where a changed ID reloads the page. The
// hold's IDs never match an app's, so a ready event from the hold
// reloads an app tab into the error page.
func (h *devHold) liveReload(w http.ResponseWriter, r *http.Request) {
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

func holdMessage(phase string) (title, lead string) {
	switch phase {
	case holdBuildFailed:
		return "Build failed", "the last build failed. Fix the error below and save; " +
			"gofastr dev rebuilds on every save. It is still running, so do not start another one."
	case holdExited:
		return "The app stopped", "the app process exited. Its output is in the terminal " +
			"running gofastr dev. Save a file to rebuild and restart it; gofastr dev is still running."
	default:
		return "Building", "building the app. This address serves the app once the build " +
			"finishes, so retry in a few seconds. gofastr dev is already running; do not start another one."
	}
}

// holdScriptURL serves holdReloadJS, the hold page's reload client. It
// reloads on any ready event whose build ID is not the page's own: the
// hold's next state, or the app's build once it is up. framework/dev's
// client cannot do this, since it ignores the first ID it sees and the
// hold sends none while building.
const holdScriptURL = "/__gofastr/dev-hold.js"

const holdReloadJS = `(() => {
  const id = document.currentScript.dataset.holdId;
  const es = new EventSource('/__livereload');
  es.addEventListener('ready', (e) => {
    if (e.data !== id) location.reload();
  });
})();`

// holdPage is the browser's view of the hold. It is the dev tool's own
// page, not an app surface: no stylesheet beyond the browser's.
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

func stripANSI(s string) string { return ansiEscape.ReplaceAllString(s, "") }

func tailString(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return "…" + strings.ToValidUTF8(s[len(s)-limit:], "")
}

// holdOutput collects what a rebuild prints for the hold page, keeping
// the last holdDetailLimit bytes. go build writes stdout and stderr from
// separate goroutines, so writes lock.
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
