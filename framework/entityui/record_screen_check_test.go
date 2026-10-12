package entityui

import (
	"bytes"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// screensHost is testHost plus the app's screen routes.
type screensHost struct {
	*testHost
	screens map[string]bool
}

func (h screensHost) HasScreen(path string) (has, known bool) { return h.screens[path], true }

func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

// #503: a list's row links point at <base>/<id>, and until the app
// registers a record screen there every one of them 404s with nothing
// said. The list logs one warning per base naming the missing route.
func TestListWarnsWhenRecordScreenMissing(t *testing.T) {
	x := newTestHost(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	const warning = "no screen serves"

	logs := captureWarnings(t)
	ui, err := New(screensHost{testHost: x.host, screens: map[string]bool{}}, Extensions{})
	if err != nil {
		t.Fatal(err)
	}
	listHTML(t, ui.List("orders"), x.ctx("/orders", ""))
	listHTML(t, ui.List("orders"), x.ctx("/orders", "?orders_page=1"))
	if n := strings.Count(logs.String(), warning); n != 1 {
		t.Fatalf("want one warning for /orders, got %d:\n%s", n, logs)
	}
	if !strings.Contains(logs.String(), "/orders/:id") {
		t.Errorf("warning should name the route to register:\n%s", logs)
	}

	// A copy from WithAPIPath keeps the check (it rebuilds the host).
	logs.Reset()
	listHTML(t, ui.WithAPIPath(func(*entity.Entity) (string, bool) { return "", false }).List("orders").Base("/desk/orders"), x.ctx("/desk", ""))
	if !strings.Contains(logs.String(), warning) {
		t.Errorf("a WithAPIPath UI must still warn for a new base:\n%s", logs)
	}

	logs.Reset()
	served, err := New(screensHost{testHost: x.host, screens: map[string]bool{"/orders/o1": true, "/orders/o2": true}}, Extensions{})
	if err != nil {
		t.Fatal(err)
	}
	listHTML(t, served.List("orders"), x.ctx("/orders", ""))
	if logs.Len() != 0 {
		t.Errorf("record screen registered: want no warning, got:\n%s", logs)
	}

	// A host that cannot see the screen routes says nothing.
	plain, err := New(x.host, Extensions{})
	if err != nil {
		t.Fatal(err)
	}
	listHTML(t, plain.List("orders"), x.ctx("/orders", ""))
	if logs.Len() != 0 {
		t.Errorf("host without HasScreen: want no warning, got:\n%s", logs)
	}
}

// countingHost counts HasScreen lookups and serves no screens.
type countingHost struct {
	*testHost
	calls *int
}

func (h countingHost) HasScreen(string) (has, known bool) { *h.calls++; return false, true }

// A NoLinks list draws no record links, so it has nothing to warn about.
func TestNoLinksListDoesNotWarn(t *testing.T) {
	x := newTestHost(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	logs := captureWarnings(t)
	ui, err := New(screensHost{testHost: x.host, screens: map[string]bool{}}, Extensions{})
	if err != nil {
		t.Fatal(err)
	}
	listHTML(t, ui.List("orders").NoLinks(), x.ctx("/orders", ""))
	if logs.Len() != 0 {
		t.Errorf("NoLinks list: want no warning, got:\n%s", logs)
	}
}

// Each base is looked up once, and the set stops growing at its cap: an
// unset Base is the request path, which a dynamic route lets callers vary.
func TestRecordScreenCheckIsBounded(t *testing.T) {
	x := newTestHost(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	logs := captureWarnings(t)
	calls := 0
	ui, err := New(countingHost{testHost: x.host, calls: &calls}, Extensions{})
	if err != nil {
		t.Fatal(err)
	}
	listHTML(t, ui.List("orders"), x.ctx("/orders", ""))
	listHTML(t, ui.List("orders"), x.ctx("/orders", "?orders_page=1"))
	if calls != 1 {
		t.Fatalf("HasScreen calls for one base = %d, want 1", calls)
	}
	for i := range maxCheckedBases + 50 {
		ui.checkRecordScreen(x.ctx("/", ""), "orders", "/u/"+strconv.Itoa(i)+"/orders", "o1")
	}
	if calls > maxCheckedBases {
		t.Errorf("HasScreen calls = %d, want at most %d", calls, maxCheckedBases)
	}
	if n := strings.Count(logs.String(), "no screen serves"); n > maxCheckedBases {
		t.Errorf("warnings = %d, want at most %d", n, maxCheckedBases)
	}
}
