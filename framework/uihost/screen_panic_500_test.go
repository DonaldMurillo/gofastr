package uihost

// Pins the screen-panic error contract: a panic in a screen's Render
// or Load is a server error — status 500 plus ONE slog Error line naming
// the path and the scrubbed panic — on every serving path (full page
// with layout, layout-less page, partial navigation, embed content
// route), never the silent 404 every render error used to take. A Load
// that RETURNS an error keeps the documented 404 but is logged at Warn;
// a path no route owns stays a plain, unlogged 404.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// rawLogHandler writes every record verbatim — level, message, one
// key=value line per attr, no quoting. slog's own TextHandler %q-escapes
// control bytes, which would HIDE a forged line behind the escaping;
// this handler keeps whatever bytes a value carried, so a control
// character that escaped the scrub shows up as a forged line.
type rawLogHandler struct{ buf *bytes.Buffer }

func (h rawLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h rawLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.buf.WriteString("level=" + r.Level.String() + " msg=" + r.Message + "\n")
	r.Attrs(func(a slog.Attr) bool {
		h.buf.WriteString(a.Key + "=" + a.Value.String() + "\n")
		return true
	})
	return nil
}

func (h rawLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h rawLogHandler) WithGroup(string) slog.Handler      { return h }

// captureLogs swaps the default logger for the raw buffer handler and
// restores the previous one when the test ends.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(rawLogHandler{buf: buf}))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// panicRenderScreen panics on Render. msg is the panic payload, carried
// separately so the CRLF-scrub arm can supply a forged-second-line
// payload.
type panicRenderScreen struct{ msg string }

func (s panicRenderScreen) Render() render.HTML { panic(s.msg) }

// panicLoadScreen panics in Load; Render is healthy.
type panicLoadScreen struct{}

func (s *panicLoadScreen) Load(context.Context) error { panic("test: load boom") }
func (s *panicLoadScreen) Render() render.HTML        { return render.Text("loaded") }

// errorLoadScreen is the control twin: Load fails through the error
// channel, which keeps the 404 contract (logged at Warn, not Error).
type errorLoadScreen struct{}

func (s *errorLoadScreen) Load(context.Context) error { return context.DeadlineExceeded }
func (s *errorLoadScreen) Render() render.HTML        { return render.Text("loaded") }

// assertPanic500 checks the shared outcome of every panic arm: 500, one
// ERROR log line naming the path and the panic, and no panic text in
// the body.
func assertPanic500(t *testing.T, tag, path, panicText string, rec *httptest.ResponseRecorder, logs *bytes.Buffer) {
	t.Helper()
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("[%s] a panicking screen must answer 500, got %d (body: %s)", tag, rec.Code, rec.Body.String())
	}
	if n := strings.Count(logs.String(), "level=ERROR"); n != 1 {
		t.Errorf("[%s] the panic must produce exactly one ERROR log line, got %d:\n%s", tag, n, logs.String())
	}
	if !strings.Contains(logs.String(), path) {
		t.Errorf("[%s] the ERROR log line must name the path %q:\n%s", tag, path, logs.String())
	}
	if panicText != "" && !strings.Contains(logs.String(), panicText) {
		t.Errorf("[%s] the ERROR log line must name the panic %q:\n%s", tag, panicText, logs.String())
	}
	if panicText != "" && strings.Contains(rec.Body.String(), panicText) {
		t.Errorf("[%s] SECURITY: panic text reached the response body:\n%s", tag, rec.Body.String())
	}
}

func TestScreenRenderPanicPageWithLayoutIs500(t *testing.T) {
	a := app.NewApp("panic500")
	a.SetDefaultLayout(app.NewLayout("ctl"))
	a.Register("/boom", panicRenderScreen{msg: "test: render boom"}, nil)
	ds := New(a)

	logs := captureLogs(t)
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))
	assertPanic500(t, "page+layout", "/boom", "test: render boom", rec, logs)
}

func TestScreenRenderPanicLayoutlessPageIs500(t *testing.T) {
	a := app.NewApp("panic500-bare")
	a.Register("/boom", panicRenderScreen{msg: "test: render boom"}, nil)
	ds := New(a)

	logs := captureLogs(t)
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))
	assertPanic500(t, "layoutless", "/boom", "test: render boom", rec, logs)
}

func TestScreenRenderPanicPartialNavigationIs500(t *testing.T) {
	a := app.NewApp("panic500-partial")
	a.SetDefaultLayout(app.NewLayout("ctl"))
	a.Register("/boom", panicRenderScreen{msg: "test: render boom"}, nil)
	ds := New(a)

	logs := captureLogs(t)
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	assertPanic500(t, "partial", "/boom", "test: render boom", rec, logs)
}

// TestScreenRenderPanicEmbedRouteIs500: the embed content route is the
// one render path that answered http.NotFound on a panic; it must carry
// the same 500 + log contract through a real grant handshake.
func TestScreenRenderPanicEmbedRouteIs500(t *testing.T) {
	application := app.NewApp("panic500-embed")
	boom := app.NewScreen("/boom", panicRenderScreen{msg: "test: embed render boom"}).WithTitle("Boom")
	application.RegisterScreen(boom, nil)

	eh, err := fembed.New(fembed.Config{
		Surfaces: []fembed.Surface{
			{Name: "boom", Screen: boom, Origins: []string{embedTestOrigin}},
		},
		BurnStore: fembed.NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("embed.New: %v", err)
	}
	eh.SetKeys([]byte("nonce-key-nonce-key-nonce-key-32"), []byte("grant-key-grant-key-grant-key-32"))
	ds := New(application, WithEmbed(eh))

	nonce, err := eh.MintNonce(context.Background(), "boom", "user-7", embedTestOrigin, nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"token": nonce, "origin": embedTestOrigin})
	exRec := httptest.NewRecorder()
	ds.ServeHTTP(exRec, httptest.NewRequest(http.MethodPost, "/__gofastr/embed-exchange", strings.NewReader(string(body))))
	if exRec.Code != http.StatusOK {
		t.Fatalf("exchange: status %d, body %s", exRec.Code, exRec.Body.String())
	}
	var out struct {
		Grant string `json:"grant"`
	}
	if err := json.Unmarshal(exRec.Body.Bytes(), &out); err != nil || out.Grant == "" {
		t.Fatalf("decode grant: %v, %+v", err, out)
	}

	logs := captureLogs(t)
	req := httptest.NewRequest(http.MethodGet, "/__gofastr/embed/boom/content", nil)
	req.Header.Set(embedGrantHeader, out.Grant)
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	assertPanic500(t, "embed", "/boom", "test: embed render boom", rec, logs)
}

func TestScreenLoadPanicPageIs500(t *testing.T) {
	a := app.NewApp("panic500-load")
	a.SetDefaultLayout(app.NewLayout("ctl"))
	a.Register("/loadboom", &panicLoadScreen{}, nil)
	ds := New(a)

	logs := captureLogs(t)
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/loadboom", nil))
	assertPanic500(t, "load-panic", "/loadboom", "test: load boom", rec, logs)
}

// TestScreenLoadErrorStays404LoggedWarn: a Load that RETURNS an error
// keeps the documented 404 contract, but is no longer silent.
func TestScreenLoadErrorStays404LoggedWarn(t *testing.T) {
	a := app.NewApp("panic500-loaderr")
	a.SetDefaultLayout(app.NewLayout("ctl"))
	a.Register("/loaderr", &errorLoadScreen{}, nil)
	ds := New(a)

	logs := captureLogs(t)
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/loaderr", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("[load-error] a Load error must keep the 404 contract, got %d", rec.Code)
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Errorf("[load-error] a Load error must be logged at Warn, got:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "/loaderr") {
		t.Errorf("[load-error] the Warn line must name the path:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), "level=ERROR") {
		t.Errorf("[load-error] a returned Load error is not a panic; no ERROR line expected:\n%s", logs.String())
	}
}

// TestUnknownPathStays404Silent: the control. A path no route owns is
// not an incident; it must keep the plain 404 and log nothing.
func TestUnknownPathStays404Silent(t *testing.T) {
	a := app.NewApp("panic500-ctl")
	a.SetDefaultLayout(app.NewLayout("ctl"))
	a.Register("/", &testHomeComp{}, nil)
	ds := New(a)

	logs := captureLogs(t)
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nowhere", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("[unknown-path] must stay 404, got %d", rec.Code)
	}
	if logs.Len() != 0 {
		t.Errorf("[unknown-path] must log nothing, got:\n%s", logs.String())
	}
}

// TestPanicProblemJSONArm: a machine client negotiating
// application/problem+json gets an RFC 9457 document at 500 that echoes
// neither the path nor the panic.
func TestPanicProblemJSONArm(t *testing.T) {
	a := app.NewApp("panic500-json")
	a.SetDefaultLayout(app.NewLayout("ctl"))
	a.Register("/boom", panicRenderScreen{msg: "test: render boom"}, nil)
	ds := New(a)

	captureLogs(t)
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	req.Header.Set("Accept", "application/problem+json")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("[problem-json] must answer 500, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("[problem-json] Content-Type must be application/problem+json, got %q", ct)
	}
	if vary := rec.Header().Get("Vary"); !strings.Contains(vary, "Accept") {
		t.Errorf("[problem-json] must carry Vary: Accept, got %q", vary)
	}
	var doc struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("[problem-json] body is not a problem document: %v\n%s", err, rec.Body.String())
	}
	if doc.Status != http.StatusInternalServerError || doc.Title == "" {
		t.Errorf("[problem-json] wrong problem document: %+v", doc)
	}
	if strings.Contains(rec.Body.String(), "/boom") || strings.Contains(rec.Body.String(), "render boom") {
		t.Errorf("[problem-json] SECURITY: problem document echoes the path or the panic:\n%s", rec.Body.String())
	}
}

// TestPanicLogScrubbed: a panic value carrying CR LF must not forge a
// second log line. The raw handler writes attr values verbatim, so an
// unscrubbed CRLF would surface as a line starting with the forged text.
func TestPanicLogScrubbed(t *testing.T) {
	a := app.NewApp("panic500-scrub")
	a.SetDefaultLayout(app.NewLayout("ctl"))
	a.Register("/boom", panicRenderScreen{msg: "boom\r\nFORGED second line"}, nil)
	ds := New(a)

	logs := captureLogs(t)
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("[scrub] must answer 500, got %d", rec.Code)
	}
	if strings.Contains(logs.String(), "\nFORGED") {
		t.Errorf("[scrub] SECURITY: panic value forged a log line:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "boom") {
		t.Errorf("[scrub] the scrubbed panic must still be identifiable:\n%s", logs.String())
	}
}
