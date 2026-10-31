package entityui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

var menuPanelIDRe = regexp.MustCompile(`aria-controls="([^"]+)"`)

// Two rows with the same title each get their own row menu. The menu's
// id used to be a hash of its label and items, so two "alpha" rows drew
// the same trigger and panel ids, and the second trigger opened the
// first row's panel (2026-10-27 bug bash U36).
func TestRowMenusSameTitleOwnIDs(t *testing.T) {
	rows := []map[string]any{
		{"id": "o1", "name": "alpha", "status": "open", "amount": "10.5", "memo": "m1"},
		{"id": "o2", "name": "alpha", "status": "open", "amount": "10.5", "memo": "m1"},
	}
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": rows},
		withAPI(map[string]string{"orders": "/api/orders"}),
	)
	h := listHTML(t, x.ui.List("orders"), x.ctx("/orders", ""))
	if n := strings.Count(h, "Actions for alpha"); n < 2 {
		t.Fatalf("want a row menu for each alpha row, found the label %d times:\n%s", n, h)
	}
	seen := map[string]bool{}
	for _, m := range menuPanelIDRe.FindAllStringSubmatch(h, -1) {
		if seen[m[1]] {
			t.Fatalf("two menus control the same panel %q:\n%s", m[1], h)
		}
		seen[m[1]] = true
	}
	for _, want := range []string{"eui-orders-actions-0-panel", "eui-orders-actions-1-panel"} {
		if !seen[want] {
			t.Errorf("no row menu controls %q; panels: %v", want, seen)
		}
	}
}
