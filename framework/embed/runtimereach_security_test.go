package embed

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A grant used to reach every path under /__gofastr, on the argument that the
// runtime endpoints there are grant-aware. Batteries mount there too
// (battery/rtc at /__gofastr/rtc, the desktop bridge at /__gofastr/desktop),
// and they are not, so a grant for a surface with no Reach opened an rtc
// signalling socket as the grant's subject. Reach into /__gofastr is now the
// list of runtime endpoints uihost mounts, nothing wider.

func grantStatus(t *testing.T, h *Host, path string) (int, bool) {
	t.Helper()
	p := &probe{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set(GrantHeader, grantFor(t, h))
	h.Middleware()(p.handler()).ServeHTTP(rec, req)
	return rec.Code, p.reached
}

func TestGrantRuntimeReachIsAllowlist(t *testing.T) {
	h := middlewareHost(t)
	for _, ok := range []string{
		"/__gofastr/action",
		"/__gofastr/sse",
		"/__gofastr/runtime.js",
		"/__gofastr/widgets",
		"/__gofastr/widget/dashboard-table",
		"/__gofastr/comp/ui/card.css",
		"/__gofastr/runtime/signals.js",
		"/__gofastr/embed-refresh",
		"/__gofastr/embed/dashboard/content",
	} {
		if code, reached := grantStatus(t, h, ok); !reached {
			t.Errorf("%s answered %d; it is a runtime endpoint an embed uses", ok, code)
		}
	}
	for _, denied := range []string{
		"/__gofastr/rtc",
		"/__gofastr/rtc/room",
		"/__gofastr/desktop/manifest.json",
		"/__gofastr/plugin/host/pluginhost.js",
		"/__gofastr/t/vendor",
		"/__gofastr/session",
		"/__gofastr/private",
		"/__gofastr/actionx",
		"/__gofastr/widgetsx",
	} {
		if code, reached := grantStatus(t, h, denied); reached || code != http.StatusForbidden {
			t.Errorf("%s answered %d (reached=%v); a grant with no Reach must not get there", denied, code, reached)
		}
	}
}

// A prefix reserved after boot (a battery mounted under an allowlisted
// runtime prefix, or anywhere else) is refused at request time too, not
// only checked against the declared Path and Reach.
func TestGrantRefusedAtReservedPrefix(t *testing.T) {
	h := middlewareHost(t)
	if err := h.AddReservedPrefixes("/__gofastr/widget/private", "/__gofastr/rtc"); err != nil {
		t.Fatalf("AddReservedPrefixes: %v", err)
	}
	for _, p := range []string{"/__gofastr/widget/private", "/__gofastr/widget/private/x", "/__gofastr/rtc"} {
		if code, reached := grantStatus(t, h, p); reached || code != http.StatusForbidden {
			t.Errorf("%s answered %d (reached=%v); it is reserved", p, code, reached)
		}
	}
	if _, reached := grantStatus(t, h, "/__gofastr/widget/public"); !reached {
		t.Error("a sibling of the reserved prefix was refused")
	}
}
