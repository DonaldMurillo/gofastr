package admin

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func paletteEnv(t *testing.T, cfg Config) *env {
	cfg.Entities = []string{"posts"}
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, cfg, nil)
	x.insert("posts", map[string]any{"id": "p1", "title": "Quarterly report", "status": "draft"})
	x.insert("posts", map[string]any{"id": "p2", "title": "Launch notes", "status": "draft"})
	return x
}

func TestPaletteFindsPagesAndRecords(t *testing.T) {
	x := paletteEnv(t, Config{Commands: []ui.PaletteCommand{{Label: "Open docs", Href: "/docs"}}})
	body := post(x.as(theAdmin), "/admin/_palette", url.Values{"q": {"report"}}).Body.String()
	if !strings.Contains(body, "Quarterly report") || !strings.Contains(body, "/admin/entities/posts/p1") {
		t.Fatalf("the palette did not find the record:\n%s", body)
	}
	if strings.Contains(body, "Launch notes") {
		t.Error("the palette matched a record that does not hold q")
	}
	all := post(x.as(theAdmin), "/admin/_palette", url.Values{"q": {""}}).Body.String()
	for _, want := range []string{"Dashboard", "/admin/entities/posts", "/admin/entities/posts/create", "/docs"} {
		if !strings.Contains(all, want) {
			t.Errorf("an empty search lacks %q", want)
		}
	}
	if strings.Contains(all, "Quarterly report") {
		t.Error("an empty search listed records")
	}
}

// The scriptless twin answers the same search as a page.
func TestSearchPageListsMatches(t *testing.T) {
	x := paletteEnv(t, Config{})
	body := get(x.as(theAdmin), "/admin/search?q=launch").Body.String()
	if !strings.Contains(body, "Launch notes") || !strings.Contains(body, `href="/admin/entities/posts/p2"`) {
		t.Fatalf("the search page lacks the match:\n%s", body)
	}
	if body := get(x.as(theAdmin), "/admin/search?q=zzz").Body.String(); !strings.Contains(body, "fui-empty") {
		t.Error("no match draws no empty state")
	}
}

func TestPaletteRecordsNeedTheGate(t *testing.T) {
	x := paletteEnv(t, Config{})
	b := x.b
	ctx := handler.SetUser(context.Background(), aReader)
	for _, c := range b.paletteCommands(ctx, "report") {
		if strings.Contains(c.Href, "/p1") {
			t.Fatal("SECURITY: paletteCommands listed a record for a caller the gate refuses")
		}
	}
}

func TestPaletteCapsTheQueryAndBody(t *testing.T) {
	x := paletteEnv(t, Config{})
	long := strings.Repeat("é", 5000)
	rr := post(x.as(theAdmin), "/admin/_palette", url.Values{"q": {long}})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("a palette body past its cap = %d, want 400", rr.Code)
	}
	// The search reads the first 200 runes: a record whose title is
	// those runes matches a longer query.
	x.insert("posts", map[string]any{"id": "p3", "title": strings.Repeat("é", 200), "status": "draft"})
	cmds := x.b.paletteCommands(handler.SetUser(context.Background(), theAdmin), strings.Repeat("é", 200)+"zzz")
	if len(cmds) != 1 || !strings.HasSuffix(cmds[0].Href, "/p3") {
		t.Errorf("the query was not cut at 200 runes: %+v", cmds)
	}
}

func TestPaletteHidesRefusedPages(t *testing.T) {
	p := page("/secret", "Secret plans", says("x"))
	p.Access = func(context.Context) bool { return false }
	x := setup(t, nil, Config{Pages: []Page{p}}, nil)
	if body := post(x.as(theAdmin), "/admin/_palette", url.Values{"q": {"secret"}}).Body.String(); strings.Contains(body, "Secret plans") {
		t.Error("the palette lists a page Access refuses")
	}
}

// An ops post past 1 MiB is refused before it is read.
func TestOpsPostBodyIsCapped(t *testing.T) {
	r := newRBACEnv(t, Config{})
	big := url.Values{"role": {"editor"}, "permission": {strings.Repeat("x", 2<<20)}}
	if rr := post(r.as(theAdmin), "/admin/rbac/_grant", big); rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("a 2 MiB grant = %d, want 413", rr.Code)
	}
	rr := rpc(r.as(theAdmin), "/admin/rbac/_grant", map[string]any{"role": "editor", "permission": strings.Repeat("x", 2<<20)})
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("a 2 MiB RPC grant = %d, want 413", rr.Code)
	}
	if len(r.auditOps("access")) != 0 {
		t.Fatal("a refused body was audited")
	}
}
