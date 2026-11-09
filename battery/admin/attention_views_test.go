package admin

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/dsl"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// funcViewEnv is the orders fixture with one view backed by a filter
// func (no Where) matching where, shown only when show says so, watched
// by the dashboard.
func funcViewEnv(t *testing.T, where string, show bool) *env {
	t.Helper()
	cfg := ordersConfig()
	cfg.Display.Views = append(cfg.Display.Views, entity.ListView{Key: "picked", Label: "Picked"})
	ext := entityui.Extensions{Entities: map[string]entityui.Extension{"orders": {
		Views: map[string]entityui.ViewFunc{"picked": {
			Filter: func(context.Context) (*filter.Predicate, error) { return dsl.ParsePredicate(where, cfg.Fields) },
			Show:   func(context.Context) bool { return show },
		}},
	}}}
	x := setupExt(t, map[string]entity.EntityConfig{"orders": cfg}, ext,
		Config{Entities: []string{"orders"}, Attention: []Watch{{Entity: "orders", View: "picked"}}}, nil)
	for _, row := range [][3]string{{"a", "10.50", "open"}, {"b", "20.00", "late"}} {
		if _, err := x.db.Exec(`INSERT INTO orders (id, ref, amount, status) VALUES (?, ?, ?, ?)`, row[0], row[0], row[1], row[2]); err != nil {
			t.Fatal(err)
		}
	}
	return x
}

// A watched view backed by a filter func is counted with the func's
// predicate, so a view no row matches leaves the panel clear; it once
// counted every row and drew the heading over an empty preview.
func TestAttentionCountsFilterFuncView(t *testing.T) {
	dash := get(funcViewEnv(t, `ref = "b"`, true).as(theAdmin), "/admin").Body.String()
	if !strings.Contains(dash, `href="/admin/entities/orders/b"`) || strings.Contains(dash, `href="/admin/entities/orders/a"`) {
		t.Errorf("the panel did not preview exactly the view's row:\n%s", dash)
	}
	dash = get(funcViewEnv(t, `ref = "none"`, true).as(theAdmin), "/admin").Body.String()
	if !strings.Contains(dash, "Nothing needs attention.") {
		t.Errorf("a view no row matches still drew in the panel:\n%s", dash)
	}
}

// SECURITY: a watched view whose Show hides it from the caller draws
// nothing. The list falls back to All for a hidden view, so the panel
// listed every row under the hidden view's heading.
func TestAttentionSkipsHiddenView(t *testing.T) {
	rr := get(funcViewEnv(t, `ref = "b"`, false).as(theAdmin), "/admin")
	if rr.Code != http.StatusOK {
		t.Fatalf("dashboard = %d", rr.Code)
	}
	dash := rr.Body.String()
	if strings.Contains(dash, `href="/admin/entities/orders/a"`) || strings.Contains(dash, "?view=picked") {
		t.Errorf("SECURITY: the panel drew a view hidden from the caller:\n%s", dash)
	}
	if !strings.Contains(dash, "Nothing needs attention.") {
		t.Errorf("the panel did not say it is clear:\n%s", dash)
	}
}
