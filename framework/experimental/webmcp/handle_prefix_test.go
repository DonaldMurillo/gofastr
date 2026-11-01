package webmcp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// TestGroupedToolPathReachesRoute: Handle registered the route under
// the group prefix but put the bare t.Path in the manifest, and the
// bridge fetches that path from the origin, so an agent's call on a
// /v1 group hit /api/echo and 404ed. The manifest path, query kept,
// must be one the router serves.
func TestGroupedToolPathReachesRoute(t *testing.T) {
	h := New()
	root := router.New()
	grp := root.Group("/v1")
	tl := validTool()
	tl.Path = "/api/echo?mode=fast"
	gotMode := ""
	if err := h.Handle(grp, tl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMode = r.URL.Query().Get("mode")
		_, _ = w.Write([]byte("ok"))
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Mount(root, nil); err != nil {
		t.Fatal(err)
	}
	tools := manifestTools(t, root)
	if len(tools) == 0 {
		t.Fatal("manifest has no tools")
	}
	if tools[0].Path != "/v1/api/echo?mode=fast" {
		t.Errorf("manifest path = %q, want /v1/api/echo?mode=fast", tools[0].Path)
	}
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, httptest.NewRequest(tools[0].Method, tools[0].Path, nil))
	if rec.Code != http.StatusOK || gotMode != "fast" {
		t.Errorf("fetching the manifest path: code %d, mode %q; want 200 and fast", rec.Code, gotMode)
	}
}
