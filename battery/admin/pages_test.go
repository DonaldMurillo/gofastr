package admin

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
)

// text is a component drawing s.
type text string

func (s text) Render() render.HTML { return render.Text(string(s)) }

func page(path, title string, build func(*http.Request) (component.Component, error)) Page {
	return Page{Path: path, Title: testPhrase(title), Build: build}
}

func says(s string) func(*http.Request) (component.Component, error) {
	return func(*http.Request) (component.Component, error) { return text(s), nil }
}

func TestPageDrawsInTheShell(t *testing.T) {
	p := page("/reports", "Reports", says("report body"))
	p.Nav = &entity.EntityNav{Group: "insights", Icon: "chart"}
	x := setup(t, nil, Config{Pages: []Page{p}}, nil)
	rr := get(x.as(theAdmin), "/admin/reports")
	body := rr.Body.String()
	if rr.Code != http.StatusOK || !strings.Contains(body, "report body") || !strings.Contains(body, "<title>Reports") {
		t.Fatalf("app page = %d:\n%s", rr.Code, body)
	}
	if !strings.Contains(body, `href="/admin/reports"`) {
		t.Error("the sidebar does not link the page")
	}
	if rr := get(x.as(aReader), "/admin/reports"); rr.Code != http.StatusForbidden {
		t.Errorf("SECURITY: a reader reached an app page: %d", rr.Code)
	}
}

func TestPageAccessRefuses(t *testing.T) {
	built := 0
	p := page("/billing", "Billing", func(*http.Request) (component.Component, error) { built++; return text("money"), nil })
	p.Nav = &entity.EntityNav{}
	p.Access = func(ctx context.Context) bool { return adminActorID(ctx) == "boss-1" }
	x := setup(t, nil, Config{Pages: []Page{p}}, nil)
	rr := get(x.as(theAdmin), "/admin/billing")
	if rr.Code != http.StatusForbidden || strings.Contains(rr.Body.String(), "money") {
		t.Fatalf("SECURITY: Access refused, the page answered %d", rr.Code)
	}
	if built != 0 {
		t.Fatal("SECURITY: Build ran for a refused caller")
	}
	if body := get(x.as(theAdmin), "/admin").Body.String(); strings.Contains(body, `href="/admin/billing"`) {
		t.Error("the sidebar links a page Access refuses")
	}
	boss := roleUser{id: "boss-1", roles: []string{"admin"}}
	if body := get(x.as(boss), "/admin/billing").Body.String(); !strings.Contains(body, "money") {
		t.Error("Access admitted, the page did not draw")
	}
}

func TestPageRefusesTheAdminsOwnPaths(t *testing.T) {
	for _, path := range []string{"/", "/queue", "/audit/x", "/rbac", "/entities/posts", "/api", "/_count", "/_new", "/search", "/account", "/account/x", "/shortcuts", "reports", "//evil", "/a?b"} {
		_, _, err := trySetup(t, nil, Config{Pages: []Page{page(path, "X", says("x"))}}, nil)
		if err == nil {
			t.Errorf("Pages path %q was accepted", path)
		}
	}
	for _, bad := range []Page{{Path: "/x", Title: testPhrase("X")}, {Path: "/x", Build: says("x")}} {
		if _, _, err := trySetup(t, nil, Config{Pages: []Page{bad}}, nil); err == nil {
			t.Errorf("page %+v was accepted", bad)
		}
	}
	twice := []Page{page("/x", "X", says("x")), page("/x", "Y", says("y"))}
	if _, _, err := trySetup(t, nil, Config{Pages: twice}, nil); err == nil {
		t.Error("a path used twice was accepted")
	}
	icon := page("/x", "X", says("x"))
	icon.Nav = &entity.EntityNav{Icon: "no-such-icon"}
	if _, _, err := trySetup(t, nil, Config{Pages: []Page{icon}}, nil); err == nil || !strings.Contains(err.Error(), "no-such-icon") {
		t.Errorf("an unregistered icon = %v", err)
	}
}

// A Build that fails, panics or returns nothing draws a generic notice
// inside the shell and logs the slot, never what it read.
func TestPageBuildFailureIsContained(t *testing.T) {
	var buf bytes.Buffer
	pages := []Page{
		page("/err", "Err", func(*http.Request) (component.Component, error) { return nil, errors.New("dsn=secret") }),
		page("/panic", "Panic", func(*http.Request) (component.Component, error) { panic("secret panic") }),
		page("/nil", "Nil", func(*http.Request) (component.Component, error) { return nil, nil }),
		page("/half", "Half", func(*http.Request) (component.Component, error) { return text("half-built secret"), errors.New("late") }),
	}
	x := setup(t, nil, Config{Pages: pages, Logger: capturingLogger(&buf)}, nil)
	for _, p := range []string{"/admin/err", "/admin/panic", "/admin/nil", "/admin/half"} {
		rr := get(x.as(theAdmin), p)
		body := rr.Body.String()
		if rr.Code != http.StatusOK || strings.Contains(body, "secret") || !strings.Contains(body, "fui-callout") {
			t.Errorf("%s = %d, leaked or drew no notice", p, rr.Code)
		}
		if !strings.Contains(body, `href="/admin"`) {
			t.Errorf("%s did not draw in the shell", p)
		}
	}
	if logs := buf.String(); strings.Contains(logs, "secret") || strings.Count(logs, "app slot failed") != 4 {
		t.Fatalf("logs = %q", logs)
	}
}

// App code runs with the caller's own context: the admin's elevation
// stops at its own pages. An app page and a dashboard card (drawn on the
// elevated dashboard) both read as the caller.
func TestPageBuildIsNotElevated(t *testing.T) {
	var eui *entityui.UI
	read := func(r *http.Request) (component.Component, error) {
		return text("count=" + eui.StatValue(r.Context(), "posts", "count", "", "", "")), nil
	}
	x := setup(t, map[string]entity.EntityConfig{"posts": lockedPosts()},
		Config{Entities: []string{"posts"}, Pages: []Page{page("/peek", "Peek", read)},
			Cards: []Card{{Key: "peek", Title: testPhrase("Peek"), Build: read}}},
		func(_ *env, c *Config) { eui = c.UI })
	x.insert("posts", map[string]any{"id": "p1", "title": "Locked one", "status": "draft"})
	h := withPolicy(x.as(theAdmin))
	if body := get(h, "/admin/_count/posts").Body.String(); !strings.Contains(body, ">1<") {
		t.Fatalf("setup: the admin's own count is not 1:\n%s", body)
	}
	if body := get(h, "/admin/peek").Body.String(); strings.Contains(body, "count=1") || !strings.Contains(body, "count=") {
		t.Fatalf("SECURITY: an app page read past the entity's Access through the admin's elevation:\n%s", body)
	}
	if body := get(h, "/admin").Body.String(); strings.Contains(body, "count=1") || !strings.Contains(body, "count=") {
		t.Fatalf("SECURITY: a dashboard card read past the entity's Access through the admin's elevation:\n%s", body)
	}
}

// A page's Access runs as the caller too, though the sidebar and the
// search page that ask it draw inside elevated screens.
func TestPageAccessIsNotElevated(t *testing.T) {
	var eui *entityui.UI
	p := page("/secret", "Secret", says("x"))
	p.Nav = &entity.EntityNav{}
	p.Access = func(ctx context.Context) bool { return eui.StatValue(ctx, "posts", "count", "", "", "") == "1" }
	x := setup(t, map[string]entity.EntityConfig{"posts": lockedPosts()},
		Config{Entities: []string{"posts"}, Pages: []Page{p}},
		func(_ *env, c *Config) { eui = c.UI })
	x.insert("posts", map[string]any{"id": "p1", "title": "Locked one", "status": "draft"})
	h := withPolicy(x.as(theAdmin))
	for _, path := range []string{"/admin", "/admin/search?q=Secret"} {
		if body := get(h, path).Body.String(); strings.Contains(body, `href="/admin/secret"`) {
			t.Errorf("SECURITY: %s asked a page's Access with the admin's elevation", path)
		}
	}
}

func TestCardsDrawAndPoll(t *testing.T) {
	cards := []Card{
		{Key: "revenue", Title: testPhrase("Revenue"), Build: says("$42"), Poll: 30 * time.Second},
		{Key: "static", Title: testPhrase("Static"), Build: says("fixed")},
	}
	x := setup(t, nil, Config{Cards: cards}, nil)
	dash := get(x.as(theAdmin), "/admin").Body.String()
	if !strings.Contains(dash, "$42") || !strings.Contains(dash, "fixed") || !strings.Contains(dash, `data-cui-poll-src="/admin/_card/revenue"`) {
		t.Fatalf("the dashboard lacks the cards:\n%s", dash)
	}
	if body := get(x.as(theAdmin), "/admin/_card/revenue").Body.String(); body != "$42" {
		t.Errorf("polled card = %q", body)
	}
	if rr := get(x.as(theAdmin), "/admin/_card/static"); rr.Code != http.StatusNotFound {
		t.Errorf("an unpolled card's route = %d, want 404", rr.Code)
	}
	if rr := get(x.as(aReader), "/admin/_card/revenue"); rr.Code != http.StatusForbidden {
		t.Errorf("SECURITY: a reader read a card: %d", rr.Code)
	}
	for _, bad := range [][]Card{
		{{Key: "Bad Key", Title: testPhrase("x"), Build: says("x")}},
		{{Key: "a", Title: testPhrase("x"), Build: says("x")}, {Key: "a", Title: testPhrase("y"), Build: says("y")}},
		{{Key: "a", Build: says("x")}},
		{{Key: "a", Title: testPhrase("x"), Build: says("x"), Poll: -time.Second}},
	} {
		if _, _, err := trySetup(t, nil, Config{Cards: bad}, nil); err == nil {
			t.Errorf("cards %+v were accepted", bad)
		}
	}
}

func TestLinksMustBeSameOrigin(t *testing.T) {
	for _, href := range []string{"https://evil.example", "//evil.example", "javascript:alert(1)", ""} {
		if _, _, err := trySetup(t, nil, Config{Links: []Link{{Label: testPhrase("x"), Href: href}}}, nil); err == nil {
			t.Errorf("link %q was accepted", href)
		}
	}
	x := setup(t, nil, Config{Links: []Link{{Label: testPhrase("Docs"), Href: "/docs", Group: "help"}}}, nil)
	if body := get(x.as(theAdmin), "/admin").Body.String(); !strings.Contains(body, `href="/docs"`) {
		t.Error("the sidebar lacks the link")
	}
}

// A page's Part is served alone at PagePartPath, for a polled region:
// the part and nothing of the shell, with the request's query, behind
// the gate and the page's own Access, and never cached.
func TestPagePartIsServedAloneBehindTheSameGates(t *testing.T) {
	built := 0
	p := page("/reports", "Reports", says("whole page"))
	p.Access = func(ctx context.Context) bool { return adminActorID(ctx) == "boss-1" }
	p.Part = func(r *http.Request) (component.Component, error) {
		built++
		return text("rows for " + r.URL.Query().Get("show")), nil
	}
	plain := page("/plain", "Plain", says("no part"))
	x := setup(t, nil, Config{Pages: []Page{p, plain}}, nil)
	path := PagePartPath("/admin/", "/reports")
	if path != "/admin/_part/reports" {
		t.Fatalf("PagePartPath = %q", path)
	}

	boss := roleUser{id: "boss-1", roles: []string{"admin"}}
	rr := get(x.as(boss), path+"?show=failed")
	body := rr.Body.String()
	if rr.Code != http.StatusOK || !strings.Contains(body, "rows for failed") || strings.Contains(body, "<html") || strings.Contains(body, "whole page") {
		t.Fatalf("the part: status %d, body %q", rr.Code, body)
	}
	if cc := rr.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q: a per-caller fragment must not be stored", cc)
	}

	built = 0
	// An admin the page's Access refuses, and a caller who is no admin.
	for name, rr := range map[string]*httptest.ResponseRecorder{
		"refused by Access": get(x.as(theAdmin), path),
		"signed out":        get(x.h, path),
	} {
		if rr.Code == http.StatusOK || strings.Contains(rr.Body.String(), "rows for") {
			t.Errorf("SECURITY: %s got the part: status %d", name, rr.Code)
		}
	}
	if built != 0 {
		t.Error("SECURITY: Part ran for a refused caller")
	}
	// A page with no Access of its own still has the admin's gate in
	// front of its part.
	open := page("/open", "Open", says("open page"))
	open.Part = says("open rows")
	y := setup(t, nil, Config{Pages: []Page{open}}, nil)
	if rr := get(y.h, PagePartPath("/admin", "/open")); rr.Code == http.StatusOK || strings.Contains(rr.Body.String(), "open rows") {
		t.Errorf("SECURITY: a signed-out caller got a part: status %d", rr.Code)
	}
	if rr := get(y.as(theAdmin), PagePartPath("/admin", "/open")); !strings.Contains(rr.Body.String(), "open rows") {
		t.Errorf("an admin did not get the part: status %d", rr.Code)
	}
	if rr := get(x.as(boss), PagePartPath("/admin", "/plain")); rr.Code != http.StatusNotFound {
		t.Errorf("a page with no Part answered %d at its part path", rr.Code)
	}
}
