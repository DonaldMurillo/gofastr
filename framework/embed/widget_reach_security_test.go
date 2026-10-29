package embed

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A widget's style, state and chrome routes are reachable only through
// the host's widget hook, which judges the name against the grant's own
// surface. With no hook every widget path is refused, and only the exact
// /core-ui/widget/<name>/<leaf> shape with a non-empty name and one of
// the three leaves reaches the hook.
func TestWidgetReachNeedsTheHook(t *testing.T) {
	get := func(h *Host, p string) (int, bool) {
		pr := &probe{}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL.Path = p
		req.Header.Set(GrantHeader, grantFor(t, h))
		rec := httptest.NewRecorder()
		h.Middleware()(pr.handler()).ServeHTTP(rec, req)
		return rec.Code, pr.reached
	}

	bare := middlewareHost(t)
	if code, reached := get(bare, "/core-ui/widget/w/state"); code != http.StatusForbidden || reached {
		t.Errorf("no hook: status %d reached=%v, want 403 and no handler", code, reached)
	}

	var asked []string
	hooked := middlewareHost(t)
	hooked.SetWidgetReach(func(name, surfacePath string) bool {
		asked = append(asked, name+"@"+surfacePath)
		return name != "v"
	})
	if code, reached := get(hooked, "/core-ui/widget/w/state"); code != http.StatusOK || !reached {
		t.Errorf("hooked, offered widget: status %d reached=%v, want 200", code, reached)
	}
	for _, p := range []string{
		"/core-ui/widget/v/state",
		"/core-ui/widget/w/rpc",
		"/core-ui/widget/v/../w/state",
		"/core-ui/widget/w",
		"/core-ui/widget//state",
		"/core-ui/widget/w/state/",
		"/state",
		"/w/state",
		"/other/widget/w/state",
	} {
		if code, reached := get(hooked, p); code == http.StatusOK || reached {
			t.Errorf("%s: status %d reached=%v, want a refusal", p, code, reached)
		}
	}
	if len(asked) < 1 || asked[0] != "w@"+hooked.surfaces["dashboard"].path {
		t.Errorf("hook calls %q, want the first to name w on the grant's surface path", asked)
	}
}
