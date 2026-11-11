package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The admin drawer, palette and shortcut sheet are scoped to admin
// paths, but the catalog listed them to anyone who asked for one: a
// signed-in reader (or an anonymous visitor, whose page view mints the
// session the catalog asks for) read the back office's widget names.
// They are listed only to a caller the admin gate admits.
func TestCatalogHidesAdminWidgets(t *testing.T) {
	setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{}, nil)
	names := func(user any, page string) []string {
		req := httptest.NewRequest(http.MethodGet, "/__gofastr/widgets?page="+page, nil)
		if user != nil {
			req = req.WithContext(handler.SetUser(req.Context(), user))
		}
		rec := httptest.NewRecorder()
		widget.ServeWidgetList(rec, req)
		var list []struct {
			Cfg struct {
				Name string `json:"name"`
			} `json:"cfg"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
			t.Fatalf("catalog: %v: %s", err, rec.Body.String())
		}
		out := make([]string, 0, len(list))
		for _, w := range list {
			out = append(out, w.Cfg.Name)
		}
		return out
	}
	// Other tests in the package mount widgets of their own; the admin's
	// are the ones the admin sees on /admin and a plain page does not.
	adminOnly := map[string]bool{}
	for _, n := range names(theAdmin, "/admin") {
		adminOnly[n] = true
	}
	for _, n := range names(theAdmin, "/elsewhere") {
		delete(adminOnly, n)
	}
	if len(adminOnly) < 3 {
		t.Fatalf("the admin's own widgets on /admin = %v, want the drawer, palette and shortcut sheet", adminOnly)
	}
	for _, who := range []struct {
		name string
		user any
	}{{"reader", aReader}, {"anonymous", nil}} {
		for _, n := range names(who.user, "/admin") {
			if adminOnly[n] {
				t.Errorf("%s's catalog on /admin lists the admin's %s", who.name, n)
			}
		}
	}
}
