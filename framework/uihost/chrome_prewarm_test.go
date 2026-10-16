package uihost

import (
	"encoding/json"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/framework/ui" // registers the chrome sheets this test reads
)

// Sidebar and page header are prewarmed, not eager: a page that renders
// neither does not block first paint on their sheets, and the catalog
// tells the runtime to fetch them at idle. ui-button stays eager
// (ToggleAction and OptimisticAction wear its classes under their own
// markers, so a scan of the page cannot see that it is needed).
func TestChromeSheetsPrewarmNotEager(t *testing.T) {
	ds := newTestUIHost()
	body := pageBody(t, ds, "/")
	head := body[:strings.Index(body, "</head>")]
	for _, n := range []string{"ui-sidebar", "ui-page-header"} {
		if blocksOn(head, n) {
			t.Errorf("%s is render-blocking on a page that does not render it:\n%s", n, head)
		}
	}
	if !blocksOn(head, "ui-button") {
		t.Errorf("ui-button lost its eager sheet:\n%s", head)
	}
	var cat map[string]struct {
		LoadMode string `json:"loadMode"`
	}
	if err := json.Unmarshal(catalogJSON(ds, ds.activeTheme(), ""), &cat); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"ui-sidebar", "ui-page-header"} {
		if got := cat[n].LoadMode; got != "prewarm" {
			t.Errorf("catalog %s loadMode = %q, want prewarm", n, got)
		}
	}
}
