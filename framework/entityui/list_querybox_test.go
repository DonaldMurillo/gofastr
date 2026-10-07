package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The query box: the filter typed by hand. A text control named the
// list's filter param rides the list's GET form; submitting navigates
// and the server parses the text with the same parser the chips use, so
// a bad text keeps the "filter did not apply" warning instead of
// failing the page.

func TestQueryBoxFiltersAndRoundTrips(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	b := x.ui.List("orders").QueryBox()
	html := listHTML(t, b, x.ctx("/orders", ""))
	// The box is named the list's filter param, prefilled, labelled, and
	// its help names the queryable fields — not the NoQuery memo.
	for _, want := range []string{
		`name="filter"`,
		"Filter",
		"name, status, amount",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the query box is missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "memo") {
		t.Errorf("the help text named the NoQuery field memo:\n%s", html)
	}
	// A typed filter narrows: only the open order remains, prefilled.
	filtered := listHTML(t, b, x.ctx("/orders", "?filter=status+%3D+%22open%22"))
	if strings.Contains(filtered, "zeta") {
		t.Errorf("the typed filter did not narrow the list:\n%s", filtered)
	}
	if !strings.Contains(filtered, "alpha") {
		t.Errorf("the typed filter dropped the matching row:\n%s", filtered)
	}
	if !strings.Contains(filtered, `value="status = &quot;open&quot;"`) {
		t.Errorf("the box did not prefill the filter text:\n%s", filtered)
	}
	// The chip still reads the same param, so both stay in sync.
	if !strings.Contains(filtered, `status = &quot;open&quot;`) {
		t.Errorf("the filter chip is not in sync with the box:\n%s", filtered)
	}
}

func TestQueryBoxKeyedUsesNamespacedParam(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	html := listHTML(t, x.ui.List("orders").Key("due").QueryBox(), x.ctx("/orders", ""))
	if !strings.Contains(html, `name="due_filter"`) {
		t.Errorf("a keyed list's query box is not namespaced:\n%s", html)
	}
}

func TestQueryBoxBadTextWarnsNotFails(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	html := listHTML(t, x.ui.List("orders").QueryBox(), x.ctx("/orders", "?filter=status+%3D"))
	if !strings.Contains(html, "Filter not applied") {
		t.Errorf("an unparseable filter lost its warning:\n%s", html)
	}
	for _, row := range []string{"alpha", "zeta"} {
		if !strings.Contains(html, row) {
			t.Errorf("the unfiltered list lost row %s:\n%s", row, html)
		}
	}
}

func TestQueryBoxOffByDefault(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	html := listHTML(t, x.ui.List("orders"), x.ctx("/orders", ""))
	if strings.Contains(html, `name="filter"`) {
		t.Errorf("the query box rendered without QueryBox():\n%s", html)
	}
}
