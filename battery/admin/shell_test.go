package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
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
		"fui-content-row--dense",
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

// An entity's nav row counts the records the admin reads, in a route
// area so a client navigation brings the new figure; HideCount drops
// it, and the phone drawer, which no navigation re-renders, draws none.
func TestNavRowsCountRecords(t *testing.T) {
	notes := notesConfig()
	notes.Display = &entity.DisplayConfig{Nav: &entity.EntityNav{HideCount: true}}
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig(), "notes": notes},
		Config{Entities: []string{"posts", "notes"}}, nil)
	x.insert("posts", map[string]any{"id": "p1", "title": "One", "status": "draft"})
	x.insert("posts", map[string]any{"id": "p2", "title": "Two", "status": "draft"})
	x.insert("notes", map[string]any{"id": "n1", "text": "hidden count"})

	row := regexp.MustCompile(`href="/admin/entities/posts"[^>]*>.*?</a>`)
	link := row.FindString(get(x.as(theAdmin), "/admin").Body.String())
	if !regexp.MustCompile(`<span class="fui-sidebar__count"><span data-cui-area="[^"]*~count-posts"[^>]*>2</span></span></a>$`).MatchString(link) {
		t.Errorf("the posts row must end in its count area holding 2:\n%s", link)
	}
	body := get(x.as(theAdmin), "/admin").Body.String()
	if strings.Contains(body, "~count-notes") {
		t.Error("HideCount still drew the notes count")
	}

	x.insert("posts", map[string]any{"id": "p3", "title": "Three", "status": "draft"})
	req := httptest.NewRequest(http.MethodGet, "/admin/entities/posts", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	req.Header.Set("X-Gofastr-From", "/admin")
	req.Header.Set("X-Gofastr-Fills", "2")
	nav := serve(x.as(theAdmin), req).Body.String()
	if !regexp.MustCompile(`data-cui-fill="[^"]*~count-posts"[^>]*>3</template>`).MatchString(nav) {
		t.Errorf("a client navigation must carry the posts count, now 3, as a fill:\n%s", nav)
	}

	if drawer := get(x.as(theAdmin), "/core-ui/widget/admin-nav/chrome").Body.String(); strings.Contains(drawer, "fui-sidebar__count") {
		t.Error("the phone drawer drew a count no navigation refreshes")
	}

	// The count reads elevated, so it asks the gate itself rather than
	// trusting the layout to run only behind it.
	posts := x.b.ents[slices.IndexFunc(x.b.ents, func(e *entity.Entity) bool { return e.GetName() == "posts" })]
	if got := x.b.navCount(handler.SetUser(context.Background(), aReader), posts); got != "" {
		t.Errorf("SECURITY: a reader outside the gate got the elevated count %q", got)
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

// "?" opens the keyboard help sheet from every admin page; without
// script its trigger is a link to the shortcuts page, which lists the
// same keys. An entity list's search takes "/".
func TestShortcutHelpSheet(t *testing.T) {
	x := setup(t, nil, Config{}, nil)
	body := get(x.as(theAdmin), "/admin").Body.String()
	for _, want := range []string{`data-cui-open="admin-keys"`, `data-hui-shortcut-click="?"`, `href="/admin/shortcuts"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the shell misses %q", want)
		}
	}
	page := get(x.as(theAdmin), "/admin/shortcuts").Body.String()
	for _, want := range []string{"Keyboard shortcuts", "Open the command palette", "Search the list", "Show these shortcuts"} {
		if !strings.Contains(page, want) {
			t.Errorf("the shortcuts page misses %q", want)
		}
	}
}

// With Config.Themes the toolbar offers a page-theme picker beside the
// light and dark toggle; without it, the toggle alone.
func TestThemePickerInToolbar(t *testing.T) {
	brutal := style.RegisterThemeOverride(theme.Brutal())
	x := setup(t, nil, Config{Themes: []ui.ThemeChoice{{Label: "Brutal", Theme: brutal}}}, nil)
	body := get(x.as(theAdmin), "/admin").Body.String()
	if !strings.Contains(body, `data-hui-theme-picker=""`) || !strings.Contains(body, ">Brutal<") {
		t.Errorf("no theme picker in the toolbar")
	}
	plain := setup(t, nil, Config{}, nil)
	if strings.Contains(get(plain.as(theAdmin), "/admin").Body.String(), "data-hui-theme-picker") {
		t.Errorf("a theme picker drew with no Themes")
	}
}
