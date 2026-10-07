package admin

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The nav drawer and the palette are widgets with routes of their own;
// both sit behind the gate, since the drawer names every exposed entity.
func TestShellWidgetsNeedTheGate(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}}, nil)
	for _, p := range []string{"/core-ui/widget/admin-nav/chrome", "/core-ui/widget/admin-palette/chrome"} {
		admin := get(x.as(theAdmin), p)
		if admin.Code == http.StatusNotFound {
			t.Fatalf("setup: %s is not a widget route (admin got 404)", p)
		}
		for _, user := range []any{nil, aReader} {
			rr := get(x.as(user), p)
			if rr.Code != http.StatusUnauthorized && rr.Code != http.StatusForbidden {
				t.Errorf("SECURITY: %v GET %s = %d; body names /admin/entities/posts: %t",
					user, p, rr.Code, strings.Contains(rr.Body.String(), "/admin/entities/posts"))
			}
		}
	}
}

func TestShellFrame(t *testing.T) {
	nav := postsConfig()
	nav.Display = &entity.DisplayConfig{Nav: &entity.EntityNav{Group: "content", Icon: "file"}}
	x := setup(t, map[string]entity.EntityConfig{"posts": nav}, Config{Entities: []string{"posts"}, Title: "Acme", SignOutPath: "/logout"},
		func(x *env, c *Config) { c.Queue = newDBQueue(t, x.db) })
	body := get(x.as(theAdmin), "/admin/queue").Body.String()
	for _, want := range []string{
		"Acme", `href="/admin/entities/posts"`, `href="/admin/queue"`, `href="/admin/audit"`,
		`aria-current="page"`, `href="/admin/search"`, "/logout",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the shell lacks %q", want)
		}
	}
	if chrome := get(x.as(theAdmin), "/core-ui/widget/admin-palette/chrome").Body.String(); !strings.Contains(chrome, "/admin/_palette") {
		t.Error("the palette does not search through /admin/_palette")
	}
	if strings.Contains(body, "<style") {
		t.Error("the admin shipped a style block")
	}
}

// Every landmark the shell draws has its own name (axe landmark-unique),
// and the toolbar row is one of them (axe region).
func TestShellLandmarksAreDistinct(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}}, nil)
	body := get(x.as(theAdmin), "/admin").Body.String()
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`<(nav|section|aside)\b[^>]*aria-label="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		key := m[1] + "/" + m[2]
		if seen[key] {
			t.Errorf("two %s landmarks named %q", m[1], m[2])
		}
		seen[key] = true
	}
	if !seen["section/Admin toolbar"] {
		t.Errorf("the toolbar is not a labelled region; landmarks = %v", seen)
	}
}

// The dashboard and the shell draw with nothing wired.
func TestShellBareAdmin(t *testing.T) {
	x := setup(t, nil, Config{}, nil)
	rr := get(x.as(theAdmin), "/admin")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Dashboard") {
		t.Fatalf("bare dashboard = %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), `href="/admin/queue"`) || strings.Contains(rr.Body.String(), `href="/admin/rbac/roles"`) {
		t.Error("the sidebar links pages nothing backs")
	}
}

func TestInitNeedsAUIHost(t *testing.T) {
	if err := New(Config{}).Init(newBareApp(t)); err == nil || !strings.Contains(err.Error(), "UI host") {
		t.Fatalf("Init without a host = %v", err)
	}
}

func TestReservedEmbedPrefixFollowsPathPrefix(t *testing.T) {
	if got := New(Config{PathPrefix: "/ops/"}).ReservedEmbedPrefixes(); len(got) != 1 || got[0] != "/ops" {
		t.Fatalf("ReservedEmbedPrefixes = %v", got)
	}
	x := setup(t, nil, Config{PathPrefix: "/ops"}, nil)
	if rr := get(x.as(nil), "/ops"); rr.Code != http.StatusUnauthorized {
		t.Errorf("anonymous /ops = %d, want 401", rr.Code)
	}
	if rr := get(x.as(theAdmin), "/ops"); rr.Code != http.StatusOK {
		t.Errorf("admin /ops = %d", rr.Code)
	}
}

// A screen re-checks the gate itself: drawn outside its policy (a host
// that mounts it elsewhere), it draws nothing and never elevates.
func TestScreenRechecksTheGate(t *testing.T) {
	x := setup(t, nil, Config{}, nil)
	s := &adminScreen{
		b:       x.b,
		title:   func(context.Context, map[string]string) string { return "T" },
		draw:    func(context.Context, map[string]string) render.HTML { return "drawn" },
		elevate: true,
	}
	reader := handler.SetUser(context.Background(), aReader)
	if s.ctx(reader) != reader {
		t.Error("SECURITY: the screen elevated a caller the gate refuses")
	}
	if got := s.RenderCtx(reader); got != "" {
		t.Errorf("SECURITY: the screen drew %q for a caller the gate refuses", got)
	}
	if s.Load(reader); s.ScreenTitle() != "" {
		t.Error("the screen resolved a title for a caller the gate refuses")
	}
	admin := handler.SetUser(context.Background(), theAdmin)
	if s.ctx(admin) == admin || s.RenderCtx(admin) != "drawn" {
		t.Error("the screen did not elevate and draw for the admin")
	}
}
