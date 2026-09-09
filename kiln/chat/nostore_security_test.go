package chat

// Pins: per-session JSON GETs carry Cache-Control: no-store — the chat
// server's own CSS arms already do, in the same file (serveWidgetCSS/
// serveBaseCSS/serveThemeCSS at server.go :189/:195/:207), where STATIC css
// is suppressed but LIVE session JSON is not.
// Surfaces: kiln/chat/server.go::serveWorld :358-385 (GET /kiln/world —
// session chat+plans+world) and ::serveStatus :239-356 (GET
// /kiln/status?fields=… — last_user/last_assistant/recent/chat/plans/app).
// Recon probe: both → 200, Cache-Control "", Vary "". The bodies carry
// per-session state including the credentialed-DSN values round-5 findings
// [#27]/[#55] showed are present in this state.
// Finding: a back/forward cache or non-conforming proxy retains one
// session's world/status JSON (sameOriginOnly is an Origin/Sec-Fetch/Host
// gate only, and kiln supports deliberate LAN binds).
// Fix direction: Cache-Control: no-store beside the Content-Type set,
// matching the CSS arms in the same file.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func TestKilnJSONNoStore(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-cc-red")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(d)) }
	l, err := live.New(journal.NewMemory(), factory)
	if err != nil {
		t.Fatal(err)
	}
	tools := protocol.New(l)
	res := tools.SetAppConfig(context.Background(), protocol.SetAppConfigArgs{
		Config: world.AppConfig{Name: "cc", DBDriver: "sqlite", DBURL: "file:red.db"},
	})
	if !res.OK {
		t.Fatalf("setup broken: SetAppConfig returned %+v", res)
	}
	srv := New(l, tools)

	legs := []struct {
		name string
		path string
	}{
		{"GET /kiln/world", "/kiln/world"},
		{"GET /kiln/status", "/kiln/status?fields=world"},
	}
	for _, leg := range legs {
		req := httptest.NewRequest(http.MethodGet, leg.path, nil)
		rec := httptest.NewRecorder()
		if leg.path == "/kiln/world" {
			srv.serveWorld(rec, req)
		} else {
			srv.serveStatus(rec, req)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("setup broken: %s status %d: %.200s", leg.name, rec.Code, rec.Body.String())
		}
		if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
			t.Errorf("SECURITY: [kiln-world-nostore] %s carries Cache-Control %q — per-session JSON (chat, plans, world incl. the credentialed-DSN state findings #27/#55 showed) with no suppression, while the same file's static CSS arms already set no-store", leg.name, cc)
		}
	}
}
