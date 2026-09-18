package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
)

type draft struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Text  string   `json:"text"`
	Tags  []string `json:"tags,omitempty"`
}

type prefs struct {
	Theme string `json:"theme"`
}

// fresh gives each test its own store namespace: New panics on a
// duplicate app id by design, and the tests share one process.
func fresh(t *testing.T, app string) *Store {
	t.Helper()
	resetForTest()
	return New(app)
}

func mustPanic(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("no panic; wanted one mentioning %q", want)
		}
		if !strings.Contains(r.(string), want) {
			t.Fatalf("panic %q does not mention %q", r, want)
		}
	}()
	fn()
}

// ─── declaration ────────────────────────────────────────────────

func TestNewValidatesAndRefusesDuplicates(t *testing.T) {
	resetForTest()
	mustPanic(t, "app id", func() { New("Site") })
	mustPanic(t, "app id", func() { New("a.b") })
	New("site")
	mustPanic(t, "already declared", func() { New("site") })
}

func TestDefineValidatesTheDeclaration(t *testing.T) {
	s := fresh(t, "site")
	mustPanic(t, "must match", func() { Define[draft](s, "Drafts", CollectionConfig{Version: 1}) })
	mustPanic(t, "Version must be 1", func() { Define[draft](s, "drafts", CollectionConfig{}) })
	mustPanic(t, "no migration to version 2", func() { Define[draft](s, "drafts", CollectionConfig{Version: 2}) })
	mustPanic(t, "outside (1, 1]", func() {
		Define[draft](s, "drafts", CollectionConfig{Version: 1, Migrations: []Migration{{Version: 2}}})
	})
	mustPanic(t, "two migrations", func() {
		Define[draft](s, "drafts", CollectionConfig{Version: 2, Migrations: []Migration{{Version: 2}, {Version: 2}}})
	})
	mustPanic(t, "zero Step", func() {
		Define[draft](s, "drafts", CollectionConfig{Version: 2, Migrations: []Migration{{Version: 2, Steps: []Step{{}}}}})
	})
	mustPanic(t, "exceeds the ceiling", func() {
		Define[draft](s, "drafts", CollectionConfig{Version: 1, MaxRecordBytes: MaxRecordBytesLimit + 1})
	})
	mustPanic(t, "exceeds MaxBytes", func() {
		Define[draft](s, "drafts", CollectionConfig{Version: 1, MaxRecordBytes: 1 << 20, MaxBytes: 1 << 19})
	})
	mustPanic(t, "does not round-trip", func() { Define[chan int](s, "chans", CollectionConfig{Version: 1}) })
	mustPanic(t, "needs an object record type", func() {
		Define[string](s, "names", CollectionConfig{Version: 1, KeyField: "id"})
	})
	Define[draft](s, "drafts", CollectionConfig{Version: 1})
	mustPanic(t, "already declared", func() { Define[draft](s, "drafts", CollectionConfig{Version: 1}) })

	// Mirror clamps to the cookie-sized defaults and ceilings.
	p := Define[prefs](s, "prefs", CollectionConfig{Version: 1, Mirror: true})
	if p.def.maxRecord != MirrorDefaultMaxRecordBytes || p.def.maxRecords != MirrorDefaultMaxRecords {
		t.Fatalf("mirror caps = %d/%d, want %d/%d", p.def.maxRecord, p.def.maxRecords, MirrorDefaultMaxRecordBytes, MirrorDefaultMaxRecords)
	}
	mustPanic(t, "exceeds the ceiling", func() {
		Define[prefs](s, "prefs2", CollectionConfig{Version: 1, Mirror: true, MaxRecordBytes: 4096})
	})
	// Every mirrored collection rides the Cookie header on every
	// request, so the store has one budget across all of them: a second
	// mirrored collection that fits on its own can still be refused.
	mustPanic(t, "over the 4096-byte budget", func() {
		Define[prefs](s, "prefs3", CollectionConfig{Version: 1, Mirror: true, MaxRecordBytes: 1024, MaxRecords: 16})
	})

	// The manifest freezes the declaration.
	_ = s.scriptJS()
	mustPanic(t, "after store", func() { Define[draft](s, "late", CollectionConfig{Version: 1}) })
}

func TestManifestCarriesTheDeclaration(t *testing.T) {
	s := fresh(t, "site")
	Define[draft](s, "drafts", CollectionConfig{
		Version: 3, KeyField: "id", MaxRecordBytes: 1024, MaxRecords: 5, MaxBytes: 4096,
		Migrations: []Migration{
			{Version: 2, Steps: []Step{Rename("body", "text"), Default("tags", []string{})}},
			{Version: 3, Steps: []Step{Remove("legacy"), Func("drafts-v3")}},
		},
	})
	Define[prefs](s, "prefs", CollectionConfig{Version: 1, Mirror: true})
	js := string(s.scriptJS())
	if !strings.HasPrefix(js, "// framework/local") || !strings.Contains(js, `window.__gofastr_local["site"] = {`) {
		t.Fatalf("manifest script shape:\n%s", js)
	}
	body := js[strings.Index(js, "] = ")+4 : len(js)-2]
	var m struct {
		Collections map[string]struct {
			V          int    `json:"v"`
			Key        string `json:"key"`
			MaxRecord  int    `json:"maxRecord"`
			MaxRecords int    `json:"maxRecords"`
			MaxBytes   int    `json:"maxBytes"`
			Mirror     bool   `json:"mirror"`
			Migrations []struct {
				V     int              `json:"v"`
				Steps []map[string]any `json:"steps"`
			} `json:"migrations"`
		} `json:"collections"`
	}
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("manifest is not JSON: %v\n%s", err, body)
	}
	d := m.Collections["drafts"]
	if d.V != 3 || d.Key != "id" || d.MaxRecord != 1024 || d.MaxRecords != 5 || d.MaxBytes != 4096 || d.Mirror {
		t.Fatalf("drafts entry = %+v", d)
	}
	if len(d.Migrations) != 2 || d.Migrations[0].V != 2 || len(d.Migrations[0].Steps) != 2 || d.Migrations[1].Steps[1]["op"] != "func" || d.Migrations[1].Steps[1]["name"] != "drafts-v3" {
		t.Fatalf("migrations = %+v", d.Migrations)
	}
	if d.Migrations[0].Steps[0]["op"] != "rename" || d.Migrations[0].Steps[0]["from"] != "body" || d.Migrations[0].Steps[0]["to"] != "text" {
		t.Fatalf("rename step = %v", d.Migrations[0].Steps[0])
	}
	p := m.Collections["prefs"]
	if !p.Mirror || p.MaxRecord != MirrorDefaultMaxRecordBytes || len(p.Migrations) != 0 {
		t.Fatalf("prefs entry = %+v", p)
	}
	if !strings.HasPrefix(s.scriptURL(), s.ScriptPath()+"?v=") || s.ScriptPath() != "/__gofastr/local/site.js" {
		t.Fatalf("ScriptURL = %q", s.scriptURL())
	}

	// The handler serves it immutably when the hash matches.
	rec := httptest.NewRecorder()
	s.ScriptHandler().ServeHTTP(rec, httptest.NewRequest("GET", s.scriptURL(), nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/javascript; charset=utf-8" || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("script handler: %d %v", rec.Code, rec.Header())
	}
	rec = httptest.NewRecorder()
	s.ScriptHandler().ServeHTTP(rec, httptest.NewRequest("GET", s.ScriptPath(), nil))
	if strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatal("an unversioned fetch must not cache immutably")
	}
}

func TestKeyRefusesWhatTheBrowserWouldRefuse(t *testing.T) {
	s := fresh(t, "site")
	d := Define[draft](s, "drafts", CollectionConfig{Version: 1, KeyField: "id"})
	mustPanic(t, "not a valid key", func() { d.Key("") })
	mustPanic(t, "not a valid key", func() { d.Key("__proto__") })
}

// ─── seed ───────────────────────────────────────────────────────

func TestSeedSignalBindsWithMarkersAndGoesGlobal(t *testing.T) {
	s := fresh(t, "site")
	d := Define[draft](s, "drafts", CollectionConfig{Version: 1})
	sl := store.JSON[draft](store.New("seedtest"), "current", draft{Title: "default"})
	seed := SeedSignal(d, "current", sl)
	if sl.Scope() != store.ScopeGlobal {
		t.Fatal("SeedSignal must imply Global: the browser's value has to survive a client navigation")
	}
	html := string(seed.Bind(context.Background(), "p", map[string]string{"class": "x"}))
	for _, want := range []string{`data-local-store="site"`, `data-local-seed="drafts:current"`, `data-fui-signal="seedtest.current"`, `class="x"`} {
		if !strings.Contains(html, want) {
			t.Fatalf("Bind lacks %s:\n%s", want, html)
		}
	}
	mustPanic(t, "not a valid key", func() { SeedSignal(d, "", sl) })
	persisted := store.JSON[draft](store.New("seedtest"), "persisted", draft{}).Persist()
	mustPanic(t, "one owner", func() { SeedSignal(d, "k", persisted) })
}

// ─── send / upload ──────────────────────────────────────────────

func TestSendAttrsNameOnlyTheDeclaration(t *testing.T) {
	s := fresh(t, "site")
	d := Define[draft](s, "drafts", CollectionConfig{Version: 1})
	p := Define[prefs](s, "prefs", CollectionConfig{Version: 1, Mirror: true})
	u := Send(d.Key("current"), p)
	a := u.Attrs()
	if a["data-local-store"] != "site" || a["data-local-send"] != "drafts:current,prefs" || a["data-fui-rpc-with"] != BridgeName {
		t.Fatalf("Attrs = %v", a)
	}
	m := u.Merge(map[string]string{"data-fui-rpc": "/x", "class": "c"})
	if m["data-fui-rpc"] != "/x" || m["class"] != "c" || m["data-local-send"] == "" {
		t.Fatalf("Merge = %v", m)
	}
	mustPanic(t, "GET", func() { u.Merge(map[string]string{"data-fui-rpc-method": "get"}) })
	mustPanic(t, "at least one", func() { Send() })
	other := fresh(t, "other")
	od := Define[draft](other, "drafts", CollectionConfig{Version: 1})
	mustPanic(t, "one store per upload", func() { Send(d, od) })
	mustPanic(t, "outside", func() { Send(d).Max(0) })
}

// echo is the wrapped handler under test: it records what it saw.
type echo struct {
	body    string
	form    url.Values
	drafts  []Record[draft]
	current draft
	found   Source
	err     error
	req     *http.Request
}

func wrapEcho(t *testing.T, u *Upload, d *Collection[draft]) (*echo, http.Handler) {
	t.Helper()
	e := &echo{}
	return e, u.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.req = app.RequestFromContext(r.Context())
		b, _ := io.ReadAll(r.Body)
		e.body = string(b)
		if r.Form != nil {
			e.form = r.PostForm
		}
		e.drafts, e.err = List(r.Context(), d)
		e.current, e.found, _ = Get(r.Context(), d, "current")
		w.WriteHeader(204)
	})
}

func TestWrapLiftsTheReservedFieldOutOfAJSONBody(t *testing.T) {
	s := fresh(t, "site")
	d := Define[draft](s, "drafts", CollectionConfig{Version: 1})
	e, h := wrapEcho(t, Send(d), d)
	body := `{"note":"hi","__local":{"drafts":[{"k":"current","v":{"id":"current","title":"T","text":"body"}},{"k":"b","v":{"id":"b","title":"B"}}]}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/up", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if e.body != `{"note":"hi"}` {
		t.Fatalf("the handler saw %q, want the body without the reserved field", e.body)
	}
	if e.req == nil {
		t.Fatal("Wrap must install app.WithRequest so cookie reads work in the handler")
	}
	if !e.found.Found() || e.current.Title != "T" || e.current.Text != "body" {
		t.Fatalf("Get(current) = %+v found=%v", e.current, e.found)
	}
	if e.err != nil || len(e.drafts) != 2 || e.drafts[0].Key != "b" || e.drafts[1].Key != "current" {
		t.Fatalf("List = %+v, %v (want two, sorted by key)", e.drafts, e.err)
	}
	if raw := carriedBy(context.Background(), s); len(raw.recs) != 0 {
		t.Fatal("a bare context carries nothing")
	}
}

func TestWrapPassesABodyWithoutTheFieldUntouched(t *testing.T) {
	s := fresh(t, "site")
	d := Define[draft](s, "drafts", CollectionConfig{Version: 1})
	e, h := wrapEcho(t, Send(d), d)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/up", strings.NewReader(`{"b":1,"a":2}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != 204 || e.body != `{"b":1,"a":2}` || e.found.Found() || len(e.drafts) != 0 {
		t.Fatalf("status %d body %q found %v drafts %v — a body with no reserved field must reach the handler byte for byte", rec.Code, e.body, e.found, e.drafts)
	}
	// An empty body, and a non-JSON content type, pass through too.
	for _, ct := range []string{"application/json", "text/plain"} {
		rec = httptest.NewRecorder()
		req = httptest.NewRequest("POST", "/up", strings.NewReader(""))
		req.Header.Set("Content-Type", ct)
		h.ServeHTTP(rec, req)
		if rec.Code != 204 {
			t.Fatalf("%s empty body: %d", ct, rec.Code)
		}
	}
}

func TestWrapRefusesWhatWasNotDeclared(t *testing.T) {
	s := fresh(t, "site")
	d := Define[draft](s, "drafts", CollectionConfig{Version: 1, MaxRecordBytes: 64, MaxRecords: 2, MaxBytes: 100})
	Define[prefs](s, "prefs", CollectionConfig{Version: 1})
	_, h := wrapEcho(t, Send(d.Key("current")), d)
	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/up", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		h.ServeHTTP(rec, req)
		return rec
	}
	cases := []struct {
		name, body string
		want       int
	}{
		{"declared key", `{"__local":{"drafts":[{"k":"current","v":{"id":"current"}}]}}`, 204},
		{"undeclared key of a declared collection", `{"__local":{"drafts":[{"k":"other","v":{}}]}}`, 400},
		{"declared store, undeclared collection", `{"__local":{"prefs":[{"k":"theme","v":"dark"}]}}`, 400},
		{"unknown collection", `{"__local":{"nope":[{"k":"x","v":1}]}}`, 400},
		{"bad key", `{"__local":{"drafts":[{"k":"__proto__","v":1}]}}`, 400},
		{"record over the cap", `{"__local":{"drafts":[{"k":"current","v":"` + strings.Repeat("x", 70) + `"}]}}`, 413},
		{"duplicate key in the body", `{"__local":{"drafts":[{"k":"current","v":1},{"k":"current","v":2}]}}`, 400},
		{"not the shape", `{"__local":[1,2]}`, 400},
		{"duplicate top-level key", `{"a":1,"A":2,"__local":{}}`, 400},
		{"missing value", `{"__local":{"drafts":[{"k":"current"}]}}`, 400},
	}
	for _, c := range cases {
		if got := post(c.body); got.Code != c.want {
			t.Errorf("%s: %d, want %d (%s)", c.name, got.Code, c.want, strings.TrimSpace(got.Body.String()))
		}
	}
	// The whole-body cap is a 413 too.
	e2, h2 := wrapEcho(t, Send(d).Max(64), d)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/up", strings.NewReader(`{"pad":"`+strings.Repeat("y", 200)+`"}`))
	req.Header.Set("Content-Type", "application/json")
	h2.ServeHTTP(rec, req)
	if rec.Code != 413 || e2.req != nil {
		t.Fatalf("over Max: %d (handler ran: %v)", rec.Code, e2.req != nil)
	}
	// Collection caps: three records where two are allowed.
	_, h3 := wrapEcho(t, Send(d), d)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/up", strings.NewReader(`{"__local":{"drafts":[{"k":"a","v":1},{"k":"b","v":2},{"k":"c","v":3}]}}`))
	req.Header.Set("Content-Type", "application/json")
	h3.ServeHTTP(rec, req)
	if rec.Code != 413 {
		t.Fatalf("over MaxRecords: %d", rec.Code)
	}
}

func TestWrapLiftsTheFieldOutOfFormBodies(t *testing.T) {
	s := fresh(t, "site")
	d := Define[draft](s, "drafts", CollectionConfig{Version: 1})
	e, h := wrapEcho(t, Send(d), d)

	form := url.Values{"note": {"hi"}, "__local": {`{"drafts":[{"k":"current","v":{"id":"current","title":"F"}}]}`}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/up", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(rec, req)
	if rec.Code != 204 || !e.found.Found() || e.current.Title != "F" {
		t.Fatalf("urlencoded: %d found=%v %+v", rec.Code, e.found, e.current)
	}
	if e.form.Get("note") != "hi" || e.form.Has("__local") {
		t.Fatalf("the handler's form = %v, want note kept and the reserved field gone", e.form)
	}

	var mb strings.Builder
	mw := multipart.NewWriter(&mb)
	_ = mw.WriteField("note", "hi")
	_ = mw.WriteField("__local", `{"drafts":[{"k":"current","v":{"id":"current","title":"M"}}]}`)
	_ = mw.Close()
	e2, h2 := wrapEcho(t, Send(d), d)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/up", strings.NewReader(mb.String()))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	h2.ServeHTTP(rec, req)
	if rec.Code != 204 || !e2.found.Found() || e2.current.Title != "M" || e2.form.Has("__local") || e2.form.Get("note") != "hi" {
		t.Fatalf("multipart: %d found=%v %+v form=%v", rec.Code, e2.found, e2.current, e2.form)
	}
}

// ─── mirror cookies ─────────────────────────────────────────────

func TestGetReadsAMirrorCookieAndIgnoresTheRest(t *testing.T) {
	s := fresh(t, "site")
	p := Define[prefs](s, "prefs", CollectionConfig{Version: 1, Mirror: true})
	d := Define[draft](s, "drafts", CollectionConfig{Version: 1})
	req := httptest.NewRequest("GET", "/", nil)
	// The browser's spelling: gofastr.local. + encodeURIComponent(app.coll.key) = encodeURIComponent(JSON).
	req.AddCookie(&http.Cookie{Name: "gofastr.local.site.prefs.theme", Value: url.PathEscape(`{"theme":"dark"}`)})
	req.AddCookie(&http.Cookie{Name: "gofastr.local.site.drafts.current", Value: url.PathEscape(`{"title":"no"}`)}) // not mirrored
	req.AddCookie(&http.Cookie{Name: "gofastr.local.other.prefs.theme", Value: url.PathEscape(`{"theme":"x"}`)})    // another app
	req.AddCookie(&http.Cookie{Name: "gofastr.local.site.prefs.bad", Value: "%7Bnot-json"})                         // malformed
	req.AddCookie(&http.Cookie{Name: "gofastr.local.site.prefs.big", Value: url.PathEscape(`"` + strings.Repeat("z", 2000) + `"`)})
	ctx := app.WithRequest(context.Background(), req)

	v, found, err := Get(ctx, p, "theme")
	if err != nil || found != SourceMirror || v.Theme != "dark" {
		t.Fatalf("Get(theme) = %+v %v %v", v, found, err)
	}
	if _, found, _ := Get(ctx, d, "current"); found.Found() {
		t.Fatal("an unmirrored collection must never be read from a cookie")
	}
	all := carriedBy(ctx, s)
	if len(all.recs) != 1 || len(all.recs["prefs"]) != 1 {
		t.Fatalf("the request carried %v — malformed, oversized, foreign and unmirrored cookies must all be ignored", all.recs)
	}
	// A record that does not decode into T is an error, not a zero value.
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.AddCookie(&http.Cookie{Name: "gofastr.local.site.prefs.theme", Value: url.PathEscape(`[1,2]`)})
	if _, found, err := Get(app.WithRequest(context.Background(), req2), p, "theme"); !found.Found() || err == nil {
		t.Fatalf("a mis-shaped record must surface as an error: found=%v err=%v", found, err)
	}
	// No request on the context: nothing, no panic.
	if _, found, _ := Get(context.Background(), p, "theme"); found.Found() {
		t.Fatal("no request, no record")
	}
	// An upload wins over the cookie for the same key.
	up := Send(p)
	var got prefs
	h := up.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got, _, _ = Get(r.Context(), p, "theme") })
	req3 := httptest.NewRequest("POST", "/", strings.NewReader(`{"__local":{"prefs":[{"k":"theme","v":{"theme":"light"}}]}}`))
	req3.Header.Set("Content-Type", "application/json")
	req3.AddCookie(&http.Cookie{Name: "gofastr.local.site.prefs.theme", Value: url.PathEscape(`{"theme":"dark"}`)})
	h.ServeHTTP(httptest.NewRecorder(), req3)
	if got.Theme != "light" {
		t.Fatalf("upload must win over the cookie: got %+v", got)
	}
}

// ─── response header ────────────────────────────────────────────

func TestPutDeleteClearAccumulateOneASCIIHeader(t *testing.T) {
	s := fresh(t, "site")
	d := Define[draft](s, "drafts", CollectionConfig{Version: 1, MaxRecordBytes: 64})
	rec := httptest.NewRecorder()
	// Above the BMP, above 0x7F, DEL, and a C0 byte: every one of them
	// has to leave the header as an escape. encoding/json escapes the C0
	// range and leaves DEL alone, and a raw DEL drops the whole header
	// under HTTP/2.
	if err := Put(rec, d, "current", draft{ID: "current", Title: "héllo 🙂 a\x7fb\x01c"}); err != nil {
		t.Fatal(err)
	}
	if err := Delete(rec, d, "old"); err != nil {
		t.Fatal(err)
	}
	if err := Clear(rec, s); err != nil {
		t.Fatal(err)
	}
	h := rec.Header().Get(ResponseHeader)
	for i := 0; i < len(h); i++ {
		if h[i] < 0x20 || h[i] > 0x7e {
			t.Fatalf("header byte %d is %#x: the header must be printable ASCII", i, h[i])
		}
	}
	var msg responseMsg
	if err := json.Unmarshal([]byte(h), &msg); err != nil {
		t.Fatalf("header is not JSON: %v\n%s", err, h)
	}
	if msg.App != "site" || len(msg.Ops) != 3 || msg.Ops[0].C != "drafts" || msg.Ops[0].K != "current" || !msg.Ops[1].D || !msg.Ops[2].Clear {
		t.Fatalf("ops = %+v", msg.Ops)
	}
	var v draft
	if err := json.Unmarshal(msg.Ops[0].V, &v); err != nil || v.Title != "héllo 🙂 a\x7fb\x01c" {
		t.Fatalf("the escaped value must decode back to the rune: %+v %v", v, err)
	}
	if err := Put(rec, d, "", draft{}); err == nil || !strings.Contains(err.Error(), "invalid key") {
		t.Fatalf("bad key: %v", err)
	}
	if err := Put(rec, d, "big", draft{Text: strings.Repeat("x", 100)}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over the record cap: %v", err)
	}
	other := fresh(t, "other")
	if err := Clear(rec, other); err == nil {
		t.Fatal("one store per response header")
	}
	rec.Header().Set(ResponseHeader, "junk")
	if err := Clear(rec, s); err == nil {
		t.Fatal("a foreign header value must not be overwritten silently")
	}
	// The accumulated header has a cap.
	rec2 := httptest.NewRecorder()
	big := Define[draft](s, "big", CollectionConfig{Version: 1, MaxRecordBytes: 8 << 10})
	var last error
	for i := 0; i < 10; i++ {
		last = Put(rec2, big, "k", draft{Text: strings.Repeat("y", 7000)})
		if last != nil {
			break
		}
	}
	if !errors.Is(last, ErrTooLarge) {
		t.Fatalf("the header must refuse past %d bytes: %v", ResponseHeaderMaxBytes, last)
	}
}

func TestClearOnNextLoadPlantsAScriptReadableBit(t *testing.T) {
	s := fresh(t, "site")
	rec := httptest.NewRecorder()
	ClearOnNextLoad(rec, httptest.NewRequest("POST", "/logout", nil), s)
	cs := rec.Result().Cookies()
	if len(cs) != 1 || cs[0].Name != "gofastr.local.clear.site" || cs[0].Value != "1" || cs[0].HttpOnly || cs[0].Secure || cs[0].SameSite != http.SameSiteLaxMode || cs[0].MaxAge != 86400 {
		t.Fatalf("cookie = %+v", cs)
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/logout", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	ClearOnNextLoad(rec, req, s)
	if !rec.Result().Cookies()[0].Secure {
		t.Fatal("Secure must follow the request scheme")
	}
}

func TestASCIIJSON(t *testing.T) {
	got := asciiJSON([]byte(`{"a":"é🙂"}`))
	if got != `{"a":"\u00e9\ud83d\ude42"}` {
		t.Fatalf("asciiJSON = %s", got)
	}
}

// A mirror cookie and an uploaded record are not the same evidence, and
// a read says which one answered.
//
// An uploaded record arrived on a request whose trigger declared it and
// whose handler is wrapped by the Upload that declared it. A mirror
// cookie is a value any script on the origin can write and any client
// can forge with curl, riding every request whether the handler asked
// for it or not. Get handed both back as found=true, with nothing tying
// the answer to Upload.Wrap, so a handler that authorised on a record
// had an unauthenticated write channel it never meant to open.
func TestAReadSaysWhetherItCameFromTheUploadOrTheMirror(t *testing.T) {
	s := fresh(t, "prov")
	p := Define[prefs](s, "prefs", CollectionConfig{Version: 1, Mirror: true})

	// A cookie anyone can plant.
	req := httptest.NewRequest("POST", "/x", nil)
	req.AddCookie(&http.Cookie{
		Name:  cookiePrefix + url.PathEscape("prov.prefs.theme"),
		Value: url.PathEscape(`{"theme":"forged"}`),
	})
	ctx := app.WithRequest(context.Background(), req)
	v, src, err := Get(ctx, p, "theme")
	if err != nil || src != SourceMirror || v.Theme != "forged" {
		t.Fatalf("Get over a cookie = %+v src=%q err=%v, want SourceMirror", v, src, err)
	}
	if !src.Found() {
		t.Fatal("a mirror read is still a read")
	}
	recs, err := List(ctx, p)
	if err != nil || len(recs) != 1 || recs[0].Source != SourceMirror {
		t.Fatalf("List = %+v %v — every record carries its own source", recs, err)
	}

	// The same key through the upload wins, and says so.
	up := withRecords(ctx, "prov", map[string]map[string]json.RawMessage{
		"prefs": {"theme": json.RawMessage(`{"theme":"declared"}`)},
	})
	v, src, err = Get(up, p, "theme")
	if err != nil || src != SourceUpload || v.Theme != "declared" {
		t.Fatalf("Get over an upload = %+v src=%q err=%v, want SourceUpload", v, src, err)
	}
	recs, _ = List(up, p)
	if len(recs) != 1 || recs[0].Source != SourceUpload {
		t.Fatalf("List = %+v — the upload wins for a key both sources carried", recs)
	}

	// And nothing at all is SourceNone, not a zero value that reads as
	// an answer.
	if _, src, _ := Get(app.WithRequest(context.Background(), httptest.NewRequest("POST", "/x", nil)), p, "theme"); src != SourceNone || src.Found() {
		t.Fatalf("an absent record = %q, want SourceNone", src)
	}
}

// The two key validators mirror each other, in the same unit.
//
// Go bounds KeyMaxLen BYTES; the browser bounded String.length, which
// counts UTF-16 code units. 200 accented characters are 200 units and
// 400 bytes, so the browser wrote the record and the server refused to
// read it: a record that exists on one side of the bridge only, with no
// error anywhere to say why. The browser now counts UTF-8 bytes too
// (validKey in local-store.js); this pins the Go half of the pair.
func TestKeyLengthIsCountedInBytesNotCharacters(t *testing.T) {
	long := strings.Repeat("é", 200) // 200 UTF-16 units, 400 UTF-8 bytes
	if len([]rune(long)) > KeyMaxLen {
		t.Fatalf("the sample is %d characters — it has to be under the cap in characters and over it in bytes", len([]rune(long)))
	}
	if validRecordKey(long) {
		t.Fatalf("a %d-byte key passed a %d-byte cap", len(long), KeyMaxLen)
	}
	if !validRecordKey(strings.Repeat("a", KeyMaxLen)) {
		t.Fatal("a key exactly at the cap must pass")
	}
}

// An undeclared collection is refused even when it carries no records.
//
// The pre-check only looked at the FIRST record, so {"nope": []},
// a collection the store never declared, with nothing in it, passed
// through and landed on the context as an empty collection a handler
// could read back. The loop below it already refused every record of an
// undeclared collection, which is what made that check both unreachable
// for the cases it was meant to cover and wrong for this one.
func TestAnUndeclaredCollectionIsRefusedEvenWhenEmpty(t *testing.T) {
	s := fresh(t, "undecl")
	d := Define[draft](s, "drafts", CollectionConfig{Version: 1})
	u := Send(d)
	if _, err := u.parse([]byte(`{"nope":[]}`)); err == nil || !strings.Contains(err.Error(), "undeclared") {
		t.Fatalf("parse({\"nope\":[]}) = %v, want an undeclared-collection refusal", err)
	}
	// A declared collection with no records is still fine.
	recs, err := u.parse([]byte(`{"drafts":[]}`))
	if err != nil || len(recs["drafts"]) != 0 {
		t.Fatalf("parse({\"drafts\":[]}) = %v %v", recs, err)
	}
}

// The Content-Length header matches the body Wrap left behind.
//
// stripJSON re-marshals the rest of the object after lifting the
// reserved field out, so the body is a different length. r.ContentLength
// was corrected and the HEADER was not, and a stale Content-Length is
// what a proxy, a middleware that re-reads the body, or a handler that
// trusts the header will believe over the reader.
func TestStripJSONLeavesNoStaleContentLength(t *testing.T) {
	s := fresh(t, "clen")
	d := Define[draft](s, "drafts", CollectionConfig{Version: 1})
	u := Send(d)
	body := `{"note":"hi","__local":{"drafts":[{"k":"a","v":{"id":"a"}}]}}`
	req := httptest.NewRequest("POST", "/x", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))

	var gotHeader string
	var gotLen int64
	var read int
	h := u.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("Content-Length")
		gotLen = r.ContentLength
		b, _ := io.ReadAll(r.Body)
		read = len(b)
	}))
	h.ServeHTTP(httptest.NewRecorder(), req)

	if read == len(body) {
		t.Fatal("the body was not rewritten — this test is not measuring what it says it is")
	}
	if gotHeader != strconv.Itoa(read) || gotLen != int64(read) {
		t.Fatalf("Content-Length header %q, r.ContentLength %d, body %d bytes — all three have to agree", gotHeader, gotLen, read)
	}
}

// The derived upload bound follows the caps the Send named, both of
// them. Summing MaxBytes alone let a collection that declares 512 bytes
// x 10 records, the POC's declaration, 5 KiB of records, render
// data-local-max="1114112": the browser's fail-closed pre-flight could
// never fire and the server accepted a megabyte for a 5 KiB collection.
func TestTheUploadBoundFollowsTheCapsItNames(t *testing.T) {
	s := fresh(t, "site")
	small := Define[draft](s, "notes", CollectionConfig{
		Version: 1, KeyField: "id", MaxRecordBytes: 512, MaxRecords: 10,
	})
	// MaxBytes was not declared, so it defaulted to 1 MiB, a size these
	// ten records can never reach.
	if got := small.def.maxBytes; got != DefaultMaxBytes {
		t.Fatalf("MaxBytes = %d, want the %d default", got, DefaultMaxBytes)
	}
	u := Send(small)
	want := 512*10 + 10*UploadRecordOverhead + UploadBodySlack
	if u.max != want {
		t.Fatalf("derived bound = %d, want %d", u.max, want)
	}
	if u.max >= 8<<10 {
		t.Fatalf("derived bound = %d: a 5 KiB collection must not declare 8 KiB or more", u.max)
	}
	if got := u.Attrs()["data-local-max"]; got != strconv.Itoa(want) {
		t.Fatalf("data-local-max = %q, want %q", got, strconv.Itoa(want))
	}
	// One key is one record, not a whole collection.
	if got := Send(small.Key("current")).max; got != 512+UploadRecordOverhead+UploadBodySlack {
		t.Fatalf("one-key bound = %d", got)
	}
	// The ceiling still holds for a declaration that really is huge.
	big := Define[draft](s, "big", CollectionConfig{
		Version: 1, MaxRecordBytes: MaxRecordBytesLimit, MaxRecords: 1000, MaxBytes: MaxBytesLimit,
	})
	if got := Send(big).max; got != UploadMaxBytesLimit {
		t.Fatalf("clamped bound = %d, want %d", got, UploadMaxBytesLimit)
	}
}

// fakeRouter records what Script's mount step registered.
type fakeRouter struct {
	pattern string
	handler http.Handler
}

func (f *fakeRouter) Get(pattern string, handler http.Handler) {
	f.pattern, f.handler = pattern, handler
}

// Serving the declaration is one expression, because the route and the
// extra script are two halves of one thing and doing only one of them
// fails silently: the manifest 404s, window.__gofastr_local stays
// undefined and localStore(app) answers null. The mount step runs
// after the URL was handed out, the order every uihost app builds in.
func TestScriptServesBothHalvesInEitherOrder(t *testing.T) {
	s := fresh(t, "site")
	Define[draft](s, "drafts", CollectionConfig{Version: 1})
	// The order every uihost app is built in: the URL for the rail
	// first, the router later.
	url, mount := s.Script()
	if want := s.scriptURL(); url != want || !strings.HasPrefix(url, s.ScriptPath()+"?v=") {
		t.Fatalf("Script returned %q, want %q with the content hash", url, want)
	}
	rt := &fakeRouter{}
	mount(rt)
	if rt.pattern != s.ScriptPath() {
		t.Fatalf("mount registered %q, want %q", rt.pattern, s.ScriptPath())
	}
	rec := httptest.NewRecorder()
	rt.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "window.__gofastr_local") {
		t.Fatalf("the mounted handler served %d: %s", rec.Code, rec.Body.String())
	}
}

// Reading a collection in a handler nobody wrapped answers an empty
// result and says nothing: the handler decides the browser sent nothing,
// and the missing Upload.Wrap is a line the compiler cannot ask for. The
// dev loop says which collection and what to do.
func TestAReadOutsideWrapWarnsInDev(t *testing.T) {
	s := fresh(t, "site")
	d := Define[draft](s, "drafts", CollectionConfig{Version: 1})
	p := Define[prefs](s, "prefs", CollectionConfig{Version: 1, Mirror: true})

	logs := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	t.Setenv("GOFASTR_DEV", "1")

	// Outside Wrap: the read is empty and the warning names the
	// collection and the fix.
	if _, src, _ := Get(context.Background(), d, "current"); src != SourceNone {
		t.Fatalf("Get outside Wrap = %q", src)
	}
	got := logs.String()
	for _, want := range []string{"outside Upload.Wrap", `app=site`, `collection=drafts`, "Upload.Wrap"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the warning is %q, missing %q", got, want)
		}
	}
	// Once per collection, not once per read.
	logs.Reset()
	_, _ = List(context.Background(), d)
	if logs.Len() != 0 {
		t.Fatalf("the warning repeated: %s", logs.String())
	}
	// A mirrored collection arrives on a cookie, with no
	// wrapper in sight: a screen render is not a mistake.
	logs.Reset()
	_, _, _ = Get(context.Background(), p, "view")
	if logs.Len() != 0 {
		t.Fatalf("a mirrored read warned: %s", logs.String())
	}
	// Inside Wrap the browser is entitled to send nothing.
	logs.Reset()
	unwrappedWarned.Clear()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/u", strings.NewReader(`{"note":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	Send(d).HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, _ = Get(r.Context(), d, "current")
	}).ServeHTTP(rec, req)
	if logs.Len() != 0 {
		t.Fatalf("a wrapped read warned: %s", logs.String())
	}
}

// Off by default: production pays one environment read, not a log line.
func TestAReadOutsideWrapIsSilentWithoutTheDevFlag(t *testing.T) {
	s := fresh(t, "site")
	d := Define[draft](s, "drafts", CollectionConfig{Version: 1})
	logs := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	t.Setenv("GOFASTR_DEV", "")
	_, _, _ = Get(context.Background(), d, "current")
	if logs.Len() != 0 {
		t.Fatalf("warned outside the dev loop: %s", logs.String())
	}
}

// ─── review round: the mirror read keeps the collection's caps ─────

// plantMirror adds one mirror cookie in the browser's spelling.
func plantMirror(req *http.Request, app, coll, key, json string) {
	req.AddCookie(&http.Cookie{Name: cookiePrefix + url.PathEscape(app+"."+coll+"."+key), Value: url.PathEscape(json)})
}

// The mirror read enforced the per-record cap and nothing else: fifty
// cookies for a collection declared at four records all landed in List.
// A cookie is a client hint, so the extras are ignored rather than
// refused, the same way an oversized or malformed one is.
func TestTheMirrorReadStopsAtTheCollectionCaps(t *testing.T) {
	s := fresh(t, "site")
	p := Define[prefs](s, "prefs", CollectionConfig{Version: 1, Mirror: true, MaxRecords: 4})
	req := httptest.NewRequest("GET", "/", nil)
	for i := range 50 {
		plantMirror(req, "site", "prefs", fmt.Sprintf("k%02d", i), `{"theme":"dark"}`)
	}
	ctx := app.WithRequest(context.Background(), req)
	got, err := List(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("List = %d records for a collection declared at 4; the mirror read counts the per-record cap only", len(got))
	}
	// Get on a key past the cap sees nothing either: the two paths agree.
	if _, src, _ := Get(ctx, p, "k49"); src.Found() {
		t.Fatal("Get read a record List refused")
	}
	if v, src, _ := Get(ctx, p, "k00"); src != SourceMirror || v.Theme != "dark" {
		t.Fatalf("Get(k00) = %+v %v", v, src)
	}

	// The byte cap: each record is 300 bytes, the collection allows 1024,
	// so three fit and the rest are dropped whatever MaxRecords says.
	s2 := fresh(t, "bytes")
	big := Define[prefs](s2, "prefs", CollectionConfig{Version: 1, Mirror: true, MaxRecordBytes: 512, MaxRecords: 8, MaxBytes: 1024})
	rec := `{"theme":"` + strings.Repeat("x", 300-12) + `"}`
	if len(rec) != 300 {
		t.Fatalf("the sample record is %d bytes, want 300", len(rec))
	}
	req2 := httptest.NewRequest("GET", "/", nil)
	for i := range 8 {
		plantMirror(req2, "bytes", "prefs", fmt.Sprintf("k%d", i), rec)
	}
	got, err = List(app.WithRequest(context.Background(), req2), big)
	if err != nil {
		t.Fatal(err)
	}
	sum := 0
	for _, r := range got {
		sum += len(r.Value.Theme) + 12
	}
	if len(got) != 3 || sum > 1024 {
		t.Fatalf("List = %d records, %d bytes, for a collection capped at 1024 bytes", len(got), sum)
	}
}

// The mirror budget is per process, because the Cookie header is per
// origin: two stores whose mirrored collections fit on their own can
// still overfill the header together, and the second Define says so,
// naming both stores.
func TestTheMirrorBudgetIsSharedAcrossStores(t *testing.T) {
	resetForTest()
	a := New("alpha")
	b := New("beta")
	// 1024 x 3 = 3072 bytes each: under 4096 alone, over it together.
	Define[prefs](a, "prefs", CollectionConfig{Version: 1, Mirror: true, MaxRecordBytes: 1024, MaxRecords: 3})
	mustPanic(t, `"alpha", "beta"`, func() {
		Define[prefs](b, "prefs", CollectionConfig{Version: 1, Mirror: true, MaxRecordBytes: 1024, MaxRecords: 3})
	})
	// An unmirrored collection on the second store costs nothing.
	Define[draft](b, "drafts", CollectionConfig{Version: 1})
	// And a store with no mirrored collection is not named.
	c := New("gamma")
	mustPanic(t, `"alpha", "gamma"`, func() {
		Define[prefs](c, "prefs", CollectionConfig{Version: 1, Mirror: true, MaxRecordBytes: 1024, MaxRecords: 3})
	})
}

// A Send that names a key beside its whole collection, in either
// order, or the same item twice, is refused at declaration: the bound
// counts the records twice and the declaration says two things.
func TestSendRefusesAnItemAnotherItemCovers(t *testing.T) {
	s := fresh(t, "site")
	d := Define[draft](s, "drafts", CollectionConfig{Version: 1})
	p := Define[prefs](s, "prefs", CollectionConfig{Version: 1})
	mustPanic(t, `collection "drafts" and key "drafts:a"`, func() { Send(d, d.Key("a")) })
	mustPanic(t, `key "drafts:a" and collection "drafts"`, func() { Send(d.Key("a"), d) })
	mustPanic(t, `key "drafts:a" and key "drafts:a"`, func() { Send(d.Key("a"), d.Key("a")) })
	mustPanic(t, `collection "drafts" and collection "drafts"`, func() { Send(d, d) })
	// Two keys of one collection, and a key beside another collection,
	// are what the shape is for.
	if got := Send(d.Key("a"), d.Key("b"), p).Attrs()["data-local-send"]; got != "drafts:a,drafts:b,prefs" {
		t.Fatalf("data-local-send = %q", got)
	}
}

// The derived bound is a product of two caps. At the ceilings that
// product is past 2^31 (100 000 records x 64 KiB is 6.5 GB), so on a
// 32-bit int it wrapped negative, and a negative bound is a
// MaxBytesReader that refuses every body and a data-local-max the
// browser reads as "nothing fits". The product is taken in int64 and
// clamped to UploadMaxBytesLimit before it narrows.
func TestTheUploadBoundIsClampedBeforeItNarrows(t *testing.T) {
	s := fresh(t, "site")
	d := Define[draft](s, "drafts", CollectionConfig{
		Version: 1, MaxRecords: MaxRecordsLimit, MaxRecordBytes: 64 << 10, MaxBytes: MaxBytesLimit,
	})
	product := int64(MaxRecordsLimit) * int64(64<<10)
	if product <= 1<<31 {
		t.Fatalf("the product is %d; it has to be past 2^31 for this test to mean anything", product)
	}
	if int64(int32(product)) >= 0 {
		t.Fatalf("the product narrows to %d on 32 bits; pick caps whose product wraps negative", int32(product))
	}
	if got := (sendItem{def: d.def}).bound(); got != UploadMaxBytesLimit {
		t.Fatalf("bound() = %d, want the %d ceiling", got, UploadMaxBytesLimit)
	}
	if got := Send(d).max; got != UploadMaxBytesLimit {
		t.Fatalf("Send(d).max = %d, want the %d ceiling", got, UploadMaxBytesLimit)
	}
}

// A mirror cookie is validated the way the upload is: the upload's
// parse refuses a duplicate key at any level with a 400, and the cookie
// with the same JSON is ignored, which is what decodeCookie does with
// everything it will not accept. json.Valid took {"theme":"a","theme":"b"}
// and the decode then read it one way on one build and the other way on
// the next.
func TestAMirrorCookieIsValidatedLikeTheUpload(t *testing.T) {
	s := fresh(t, "site")
	p := Define[prefs](s, "prefs", CollectionConfig{Version: 1, Mirror: true})
	dup := `{"theme":"a","theme":"b"}`
	req := httptest.NewRequest("GET", "/", nil)
	plantMirror(req, "site", "prefs", "theme", dup)
	plantMirror(req, "site", "prefs", "folded", `{"theme":"a","Theme":"b"}`)
	plantMirror(req, "site", "prefs", "two", `{"theme":"a"} {"theme":"b"}`)
	plantMirror(req, "site", "prefs", "fine", `{"theme":"a"}`)
	ctx := app.WithRequest(context.Background(), req)
	for _, key := range []string{"theme", "folded", "two"} {
		if v, src, err := Get(ctx, p, key); src.Found() || err != nil {
			t.Fatalf("Get(%s) = %+v %v %v; a cookie the upload would refuse is ignored, not read", key, v, src, err)
		}
	}
	got, err := List(ctx, p)
	if err != nil || len(got) != 1 || got[0].Key != "fine" {
		t.Fatalf("List = %+v %v, want only the well-formed record", got, err)
	}
	// The same bytes through the upload are a 400, so the two sources
	// draw the line in the same place.
	rec := httptest.NewRecorder()
	body := `{"__local":{"prefs":[{"k":"theme","v":` + dup + `}]}}`
	up := httptest.NewRequest("POST", "/", strings.NewReader(body))
	up.Header.Set("Content-Type", "application/json")
	Send(p).HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("the handler ran on a duplicate key") }).ServeHTTP(rec, up)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("upload with a duplicate key = %d, want 400", rec.Code)
	}
	// Strict is about the JSON, not about T: a well-formed record with a
	// field T does not declare still reads, the way an upload does. The
	// browser is allowed to keep more than the Go type reads.
	req2 := httptest.NewRequest("GET", "/", nil)
	plantMirror(req2, "site", "prefs", "theme", `{"theme":"a","extra":1}`)
	ctx2 := app.WithRequest(context.Background(), req2)
	if v, src, err := Get(ctx2, p, "theme"); src != SourceMirror || err != nil || v.Theme != "a" {
		t.Fatalf("Get with an extra field = %+v %v %v, want the record", v, src, err)
	}
}
