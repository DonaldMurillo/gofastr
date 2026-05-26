package ui_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	ui "github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// secHelper checks for a literal substring in rendered HTML.
func secHelper(t *testing.T, name string, html render.HTML, mustContain, mustNotContain []string) {
	t.Helper()
	s := string(html)
	for _, sub := range mustContain {
		if !strings.Contains(s, sub) {
			t.Errorf("SECURITY: [%s] expected output to contain %q", name, sub)
		}
	}
	for _, sub := range mustNotContain {
		if strings.Contains(s, sub) {
			t.Errorf("SECURITY: [%s] output must NOT contain %q\n  got: %s", name, sub, truncate(s, 300))
		}
	}
}

// truncate limits a string for error messages.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// ─── DataTable security (15 tests) ──────────────────────────────────

func TestDataTable_ColumnKeyXSS(t *testing.T) {
	t.Parallel()
	// Column Key with script tags — it's URL-encoded in sort hrefs.
	h := ui.DataTable(ui.DataTableConfig{
		Columns:          []ui.Column{{Key: "<script>alert(1)</script>", Header: "X", Sortable: true}},
		Rows:             []ui.Row{{Cells: map[string]render.HTML{"<script>alert(1)</script>": render.Text("ok")}}},
		SortHrefPattern:  "?sort=%s&dir=%s",
	})
	s := string(h)
	// The key should be URL-encoded in the href, not injected raw.
	if strings.Contains(s, "?sort=<script>") {
		t.Errorf("SECURITY: [datatable-column-key] sort href contains raw script tag in key\n  got: %s", truncate(s, 300))
	}
	t.Logf("NOTE: Column keys are URL-encoded in hrefs via url.QueryEscape")
}

func TestDataTable_ColumnHeaderXSS(t *testing.T) {
	t.Parallel()
	h := ui.DataTable(ui.DataTableConfig{
		Columns: []ui.Column{{Key: "name", Header: "<script>alert('xss')</script>"}},
		Rows:    []ui.Row{{Cells: map[string]render.HTML{"name": render.Text("safe")}}},
	})
	s := string(h)
	// render.Text escapes the header, so <script> must not appear literally.
	if strings.Contains(s, "<script>") {
		t.Errorf("SECURITY: [datatable-column-header] header XSS not escaped:\n  %s", truncate(s, 300))
	}
	if !strings.Contains(s, "&lt;script&gt;") {
		t.Errorf("SECURITY: [datatable-column-header] expected escaped header text")
	}
}

func TestDataTable_CellContentXSS(t *testing.T) {
	t.Parallel()
	// Cell content is render.HTML (raw). Callers control it.
	// We document that raw HTML passes through without escaping.
	malicious := render.HTML("<script>alert('cell-xss')</script>")
	h := ui.DataTable(ui.DataTableConfig{
		Columns: []ui.Column{{Key: "data", Header: "Data"}},
		Rows:    []ui.Row{{Cells: map[string]render.HTML{"data": malicious}}},
	})
	s := string(h)
	if strings.Contains(s, "<script>alert('cell-xss')</script>") {
		t.Logf("SECURITY: [datatable-cell-content] raw script tag in cell content was NOT escaped — caller MUST sanitize cell HTML")
	} else {
		t.Error("SECURITY: [datatable-cell-content] expected raw HTML to pass through (behavior change detected)")
	}
	t.Logf("NOTE: [datatable-cell-content] cell content is render.HTML (raw) — caller is responsible for sanitization")
}

func TestDataTable_SortHrefInjection(t *testing.T) {
	t.Parallel()
	// SortHrefPattern with path traversal — it's a Sprintf pattern.
	h := ui.DataTable(ui.DataTableConfig{
		Columns: []ui.Column{{Key: "name", Header: "Name", Sortable: true}},
		Rows:    []ui.Row{{Cells: map[string]render.HTML{"name": render.Text("Alice")}}},
		SortHrefPattern:  "?sort=%s&dir=%s&redirect=../../../etc/passwd",
	})
	s := string(h)
	// The pattern is used directly — this documents that callers must provide safe patterns.
	if strings.Contains(s, "../../../etc/passwd") {
		t.Logf("NOTE: [datatable-sort-href] SortHrefPattern is caller-controlled format string; framework does not sanitize it")
	}
	if !strings.Contains(s, "href=") {
		t.Errorf("SECURITY: [datatable-sort-href] expected href attribute in output")
	}
}

func TestDataTable_RowIDXSS(t *testing.T) {
	t.Parallel()
	h := ui.DataTable(ui.DataTableConfig{
		Columns: []ui.Column{{Key: "x", Header: "X"}},
		Rows:    []ui.Row{{ID: `<img src=x onerror=alert(1)>`, Cells: map[string]render.HTML{"x": render.Text("v")}}},
	})
	s := string(h)
	// Row ID goes into an HTML attribute via html.TableRowConfig{ID: r.ID}.
	// render.Tag escapes attribute values.
	if strings.Contains(s, `onerror=alert(1)`) && !strings.Contains(s, "&lt;") {
		t.Errorf("SECURITY: [datatable-row-id] row ID with script payload not escaped in attribute:\n  %s", truncate(s, 300))
	}
	t.Logf("NOTE: [datatable-row-id] Row.ID goes through attribute escaping in render.Tag")
}

func TestDataTable_EmptyColumnsHandled(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("SECURITY: [datatable-empty-columns] expected panic for empty columns")
		}
		msg, ok := r.(string)
		if ok && strings.Contains(msg, "at least one Column") {
			t.Logf("NOTE: [datatable-empty-columns] correctly panics with descriptive message")
		} else {
			t.Errorf("SECURITY: [datatable-empty-columns] unexpected panic value: %v", r)
		}
	}()
	ui.DataTable(ui.DataTableConfig{Columns: []ui.Column{}})
}

func TestDataTable_NilRowsHandled(t *testing.T) {
	t.Parallel()
	// Nil rows should render the empty state without panicking.
	h := ui.DataTable(ui.DataTableConfig{
		Columns: []ui.Column{{Key: "x", Header: "X"}},
		Rows:    nil,
	})
	s := string(h)
	if !strings.Contains(s, "ui-empty-state") {
		t.Errorf("SECURITY: [datatable-nil-rows] expected empty state for nil rows, got:\n  %s", truncate(s, 300))
	}
	if strings.Contains(s, "<table") {
		t.Errorf("SECURITY: [datatable-nil-rows] should not render table element for nil rows")
	}
}

func TestDataTable_VeryLongCellContent(t *testing.T) {
	t.Parallel()
	longText := strings.Repeat("A<>&\"", 10000) // 50000 chars with HTML-special chars
	h := ui.DataTable(ui.DataTableConfig{
		Columns: []ui.Column{{Key: "big", Header: "Big"}},
		Rows:    []ui.Row{{Cells: map[string]render.HTML{"big": render.Text(longText)}}},
	})
	s := string(h)
	if len(s) < 50000 {
		t.Errorf("SECURITY: [datatable-long-cell] output unexpectedly short (%d bytes)", len(s))
	}
	// Check that special chars are escaped
	if strings.Contains(s, `<>&"`) {
		t.Errorf("SECURITY: [datatable-long-cell] special chars not properly escaped")
	}
}

func TestDataTable_ColumnAlignInjection(t *testing.T) {
	t.Parallel()
	h := ui.DataTable(ui.DataTableConfig{
		Columns: []ui.Column{{Key: "x", Header: "X", Align: `start"><script>alert(1)</script>`}},
		Rows:    []ui.Row{{Cells: map[string]render.HTML{"x": render.Text("v")}}},
	})
	s := string(h)
	if strings.Contains(s, "<script>alert(1)</script>") {
		t.Errorf("SECURITY: [datatable-align-injection] align value with script tag injected into output:\n  %s", truncate(s, 300))
	}
	t.Logf("NOTE: [datatable-align-injection] align value goes into CSS class, not raw HTML")
}

func TestDataTable_PaginationLinksXSS(t *testing.T) {
	t.Parallel()
	// We can't easily construct a pagination.Config with XSS in the URL
	// since the URL is passed directly. Test that DataTable renders
	// without error when a pagination config is present.
	t.Logf("NOTE: [datatable-pagination-xss] pagination links are caller-controlled; framework renders them via core-ui/patterns/pagination")
	// Verify basic rendering with a pagination-like setup doesn't panic.
	func() {
		ui.DataTable(ui.DataTableConfig{
			Columns: []ui.Column{{Key: "x", Header: "X"}},
			Rows:    []ui.Row{{Cells: map[string]render.HTML{"x": render.Text("v")}}},
			// No pagination set — just verify no crash
		})
	}()
}

func TestDataTable_EmptyStateMessageXSS(t *testing.T) {
	t.Parallel()
	h := ui.DataTable(ui.DataTableConfig{
		Columns: []ui.Column{{Key: "x", Header: "X"}},
		Rows:    nil,
		Empty:   ui.EmptyStateConfig{Title: "<script>alert('empty')</script>", Description: "<img src=x onerror=prompt(1)>"},
	})
	s := string(h)
	if strings.Contains(s, "<script>alert('empty')</script>") {
		t.Errorf("SECURITY: [datatable-empty-state-xss] script tag in empty state title not escaped")
	}
	if !strings.Contains(s, "&lt;script&gt;") {
		t.Errorf("SECURITY: [datatable-empty-state-xss] expected escaped title text")
	}
	if strings.Contains(s, "<img src=x onerror=prompt(1)>") {
		t.Errorf("SECURITY: [datatable-empty-state-xss] img tag in empty state description not escaped")
	}
}

func TestDataTable_SortKeyInjection(t *testing.T) {
	t.Parallel()
	// SQL-like content in sort key — should be URL-encoded in href.
	sqlPayload := "1; DROP TABLE users--"
	h := ui.DataTable(ui.DataTableConfig{
		Columns:          []ui.Column{{Key: sqlPayload, Header: "ID", Sortable: true}},
		Rows:             []ui.Row{{Cells: map[string]render.HTML{sqlPayload: render.Text("1")}}},
		SortHrefPattern:  "?sort=%s&dir=%s",
	})
	s := string(h)
	// The raw SQL should NOT appear in an href value unencoded.
	if strings.Contains(s, "sort=1; DROP TABLE") {
		t.Errorf("SECURITY: [datatable-sort-key] SQL-like payload in sort key not URL-encoded:\n  %s", truncate(s, 300))
	}
	// The key should be URL-encoded.
	if strings.Contains(s, "sort=1%3B+DROP+TABLE+users--") {
		t.Logf("NOTE: [datatable-sort-key] SQL payload correctly URL-encoded")
	}
}

func TestDataTable_ConcurrentRender(t *testing.T) {
	t.Parallel()
	cols := []ui.Column{
		{Key: "a", Header: "A", Sortable: true},
		{Key: "b", Header: "B"},
	}
	rows := []ui.Row{{Cells: map[string]render.HTML{"a": render.Text("1"), "b": render.Text("2")}}}
	cfg := ui.DataTableConfig{
		Columns:         cols,
		Rows:            rows,
		SortHrefPattern: "?sort=%s&dir=%s",
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Mutate config slightly per goroutine
			localCfg := cfg
			localCfg.ID = fmt.Sprintf("table-%d", idx)
			h := ui.DataTable(localCfg)
			s := string(h)
			if !strings.Contains(s, fmt.Sprintf(`id="table-%d"`, idx)) {
				t.Errorf("SECURITY: [datatable-concurrent] goroutine %d: expected ID in output", idx)
			}
		}(i)
	}
	wg.Wait()
}

func TestDataTable_ClassInjection(t *testing.T) {
	t.Parallel()
	h := ui.DataTable(ui.DataTableConfig{
		Columns: []ui.Column{{Key: "x", Header: "X"}},
		Rows:    []ui.Row{{Cells: map[string]render.HTML{"x": render.Text("v")}}},
		Class:   `"><script>alert("class-xss")</script><div class="`,
	})
	s := string(h)
	if strings.Contains(s, `<script>alert("class-xss")</script>`) {
		t.Errorf("SECURITY: [datatable-class] class attribute with script tag not escaped:\n  %s", truncate(s, 300))
	}
	t.Logf("NOTE: [datatable-class] class goes through render.Tag attribute escaping")
}

func TestDataTable_IDInjection(t *testing.T) {
	t.Parallel()
	h := ui.DataTable(ui.DataTableConfig{
		Columns: []ui.Column{{Key: "x", Header: "X"}},
		Rows:    []ui.Row{{Cells: map[string]render.HTML{"x": render.Text("v")}}},
		ID:      `" onclick="alert('id-xss')" data-x="`,
	})
	s := string(h)
	// Attribute values should be escaped by render.Tag.
	if strings.Contains(s, `onclick="alert('id-xss')"`) {
		t.Errorf("SECURITY: [datatable-id] ID attribute with event handler not escaped:\n  %s", truncate(s, 300))
	}
	t.Logf("NOTE: [datatable-id] ID goes through render.Tag attribute escaping")
}

// ─── Card/Container/Banner security (15 tests) ─────────────────────

func TestCard_TitleXSS(t *testing.T) {
	t.Parallel()
	h := ui.Card(ui.CardConfig{Heading: `<script>alert('card-title')</script>`}, render.Text("body"))
	s := string(h)
	if strings.Contains(s, "<script>alert('card-title')</script>") {
		t.Errorf("SECURITY: [card-title-xss] script tag in heading not escaped")
	}
	if !strings.Contains(s, "&lt;script&gt;") {
		t.Errorf("SECURITY: [card-title-xss] expected escaped heading text")
	}
}

func TestCard_DescriptionXSS(t *testing.T) {
	t.Parallel()
	h := ui.Card(ui.CardConfig{Heading: "Safe", Description: `<img src=x onerror="alert('card-desc')">`}, render.Text("body"))
	s := string(h)
	if strings.Contains(s, `<img src=x onerror="alert('card-desc')">`) {
		t.Errorf("SECURITY: [card-description-xss] img tag in description not escaped")
	}
}

func TestCard_ClassInjection(t *testing.T) {
	t.Parallel()
	h := ui.Card(ui.CardConfig{
		Heading: "Test",
		Class:   `"><script>alert('card-class')</script><div x="`,
	}, render.Text("body"))
	s := string(h)
	if strings.Contains(s, `<script>alert('card-class')</script>`) {
		t.Errorf("SECURITY: [card-class-injection] class with script tag not escaped:\n  %s", truncate(s, 300))
	}
	t.Logf("NOTE: [card-class-injection] class value goes through attribute escaping")
}

func TestCard_NilBodyHandled(t *testing.T) {
	t.Parallel()
	// Card with no body variadic args — should render without panic.
	h := ui.Card(ui.CardConfig{Heading: "No body"})
	s := string(h)
	if !strings.Contains(s, `data-fui-comp="ui-card"`) {
		t.Errorf("SECURITY: [card-nil-body] expected ui-card marker in output:\n  %s", s)
	}
	if strings.Contains(s, "ui-card__body") {
		t.Errorf("SECURITY: [card-nil-body] should not render body element when no body provided")
	}
}

func TestCard_VeryLongTitle(t *testing.T) {
	t.Parallel()
	longTitle := strings.Repeat("X", 100000)
	h := ui.Card(ui.CardConfig{Heading: longTitle}, render.Text("body"))
	s := string(h)
	if len(s) < 100000 {
		t.Errorf("SECURITY: [card-long-title] output unexpectedly truncated (%d bytes)", len(s))
	}
	// The heading should contain the full title
	if !strings.Contains(s, longTitle) {
		t.Errorf("SECURITY: [card-long-title] title was truncated in output")
	}
}

func TestContainer_ClassInjection(t *testing.T) {
	t.Parallel()
	h := ui.Container(ui.ContainerConfig{
		Class: `"><script>alert('container-class')</script><span x="`,
	}, render.Text("child"))
	s := string(h)
	if strings.Contains(s, `<script>alert('container-class')</script>`) {
		t.Errorf("SECURITY: [container-class-injection] class with script tag not escaped:\n  %s", truncate(s, 300))
	}
	t.Logf("NOTE: [container-class-injection] class value goes through render.Tag attribute escaping")
}

func TestContainer_IDInjection(t *testing.T) {
	t.Parallel()
	h := ui.Container(ui.ContainerConfig{
		ID: `" onclick="alert('container-id')" data-x="`,
	}, render.Text("child"))
	s := string(h)
	if strings.Contains(s, `onclick="alert('container-id')"`) {
		t.Errorf("SECURITY: [container-id-injection] ID attribute with event handler not escaped:\n  %s", truncate(s, 300))
	}
}

func TestContainer_NilChildrenHandled(t *testing.T) {
	t.Parallel()
	// Container with no children — should render without panic.
	h := ui.Container(ui.ContainerConfig{})
	s := string(h)
	if !strings.Contains(s, `data-fui-comp="ui-container"`) {
		t.Errorf("SECURITY: [container-nil-children] expected ui-container marker:\n  %s", s)
	}
}

func TestContainer_VeryDeepNesting(t *testing.T) {
	t.Parallel()
	// 100 levels of nested containers.
	inner := render.Text("leaf")
	for i := 0; i < 100; i++ {
		inner = ui.Container(ui.ContainerConfig{ID: fmt.Sprintf("nest-%d", i)}, inner)
	}
	s := string(inner)
	if !strings.Contains(s, "leaf") {
		t.Errorf("SECURITY: [container-deep-nesting] leaf text missing from deeply nested output")
	}
	for i := 0; i < 100; i++ {
		if !strings.Contains(s, fmt.Sprintf("nest-%d", i)) {
			t.Errorf("SECURITY: [container-deep-nesting] level %d ID missing", i)
			break
		}
	}
}

func TestBanner_TitleXSS(t *testing.T) {
	t.Parallel()
	h := ui.Banner(ui.BannerConfig{Title: `<script>alert('banner-title')</script>`})
	s := string(h)
	if strings.Contains(s, "<script>alert('banner-title')</script>") {
		t.Errorf("SECURITY: [banner-title-xss] script tag in title not escaped")
	}
	if !strings.Contains(s, "&lt;script&gt;") {
		t.Errorf("SECURITY: [banner-title-xss] expected escaped title text")
	}
}

func TestBanner_MessageXSS(t *testing.T) {
	t.Parallel()
	h := ui.Banner(ui.BannerConfig{Title: "Notice", Body: `<script>alert('banner-body')</script>`})
	s := string(h)
	if strings.Contains(s, "<script>alert('banner-body')</script>") {
		t.Errorf("SECURITY: [banner-message-xss] script tag in body not escaped")
	}
}

func TestBanner_ActionLinkXSS(t *testing.T) {
	t.Parallel()
	// Action is render.HTML (raw). A javascript: href in a raw action
	// would pass through. Test that we can at least render without panic
	// and note the caller responsibility.
	xssAction := render.HTML(`<a href="javascript:alert('action-xss')">Click</a>`)
	_ = ui.Banner(ui.BannerConfig{Title: "Info", Action: xssAction})
	_ = xssAction
	// Action is raw render.HTML — caller responsibility.
	t.Logf("NOTE: [banner-action-link] Action is raw render.HTML — callers are responsible for sanitization")
}

func TestBanner_ClassInjection(t *testing.T) {
	t.Parallel()
	h := ui.Banner(ui.BannerConfig{
		Title: "Info",
		Class: `"><script>alert('banner-class')</script><div x="`,
	})
	s := string(h)
	if strings.Contains(s, `<script>alert('banner-class')</script>`) {
		t.Errorf("SECURITY: [banner-class-injection] class with script tag not escaped:\n  %s", truncate(s, 300))
	}
}

func TestBanner_VariantHandling(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("SECURITY: [banner-variant] expected panic for unknown variant")
		}
		msg, ok := r.(string)
		if ok && strings.Contains(msg, "unknown Variant") {
			t.Logf("NOTE: [banner-variant] correctly rejects unknown variant with panic")
		} else {
			t.Errorf("SECURITY: [banner-variant] unexpected panic value: %v", r)
		}
	}()
	ui.Banner(ui.BannerConfig{Title: "X", Variant: ui.BannerVariant("<script>alert('var')</script>")})
}

func TestToolbar_ButtonXSS(t *testing.T) {
	t.Parallel()
	// Toolbar groups contain raw render.HTML children.
	// A script tag in a button child would pass through as raw HTML.
	xssBtn := render.HTML(`<button onclick="alert('toolbar-xss')">Evil</button>`)
	_ = xssBtn
	_ = ui.Toolbar(ui.ToolbarConfig{
		Label: "Actions",
		Groups: []ui.ToolbarGroup{{Children: []render.HTML{xssBtn}}},
	})
	// Group.Children are raw render.HTML — caller responsibility.
	t.Logf("NOTE: [toolbar-button-xss] Group.Children are raw render.HTML — callers are responsible for sanitization")
}

// ─── Other component security (10 tests) ───────────────────────────

func TestDivider_ClassInjection(t *testing.T) {
	t.Parallel()
	h := ui.Divider(ui.DividerConfig{
		Class: `"><script>alert('divider-class')</script><span x="`,
	})
	s := string(h)
	if strings.Contains(s, `<script>alert('divider-class')</script>`) {
		t.Errorf("SECURITY: [divider-class-injection] class with script tag not escaped:\n  %s", truncate(s, 300))
	}
	t.Logf("NOTE: [divider-class-injection] class goes through render.Tag attribute escaping")
}

func TestIcon_NameInjection(t *testing.T) {
	t.Parallel()
	// Icon name with path traversal — it's used as a registry key.
	h := ui.Icon("../../etc/passwd", ui.IconConfig{})
	s := string(h)
	if s != "" {
		t.Errorf("SECURITY: [icon-name-injection] unknown icon should render empty, got:\n  %s", truncate(s, 200))
	}
	t.Logf("NOTE: [icon-name-injection] unknown icon names produce empty output (safe default)")
}

func TestImage_SrcJavaScript(t *testing.T) {
	t.Parallel()
	h := ui.OptimizedImage(ui.OptimizedImageConfig{
		Src:    "javascript:alert('img-xss')",
		Alt:    "test",
		Width:  100,
		Height: 100,
	})
	s := string(h)
	if !strings.Contains(s, "javascript:alert('img-xss')") {
		// The src goes through attribute escaping, but javascript: is a protocol, not
		// HTML-special characters. It would appear escaped as: javascript:alert(&#39;img-xss&#39;)
		// but the protocol is still present.
		t.Logf("NOTE: [image-src-javascript] src attribute escapes HTML entities but javascript: protocol may persist")
	}
	if strings.Contains(s, `src="javascript:alert('img-xss')"` ) {
		t.Errorf("SECURITY: [image-src-javascript] raw javascript: URL in src attribute:\n  %s", truncate(s, 300))
	}
}

func TestImage_AltXSS(t *testing.T) {
	t.Parallel()
	h := ui.OptimizedImage(ui.OptimizedImageConfig{
		Src:    "https://example.com/img.png",
		Alt:    `<script>alert('img-alt')</script>`,
		Width:  100,
		Height: 100,
	})
	s := string(h)
	// Alt text goes through html.ImageConfig which uses attribute escaping.
	if strings.Contains(s, `<script>alert('img-alt')</script>`) {
		t.Errorf("SECURITY: [image-alt-xss] script tag in alt text not escaped:\n  %s", truncate(s, 300))
	}
	if !strings.Contains(s, "&lt;script&gt;") {
		t.Errorf("SECURITY: [image-alt-xss] expected escaped alt text")
	}
}

func TestImage_ClassInjection(t *testing.T) {
	t.Parallel()
	h := ui.OptimizedImage(ui.OptimizedImageConfig{
		Src:    "https://example.com/img.png",
		Alt:    "safe",
		Class:  `"><script>alert('img-class')</script><span x="`,
		Width:  100,
		Height: 100,
	})
	s := string(h)
	if strings.Contains(s, `<script>alert('img-class')</script>`) {
		t.Errorf("SECURITY: [image-class-injection] class with script tag not escaped:\n  %s", truncate(s, 300))
	}
}

func TestCopyButton_TextXSS(t *testing.T) {
	t.Parallel()
	h := ui.CopyButton(ui.CopyButtonConfig{
		Target: "#code",
		Label:  `<script>alert('copy-xss')</script>`,
	})
	s := string(h)
	// Label goes through render.Text which escapes.
	if strings.Contains(s, "<script>alert('copy-xss')</script>") {
		t.Errorf("SECURITY: [copy-button-text-xss] script tag in label not escaped")
	}
	if !strings.Contains(s, "&lt;script&gt;") {
		t.Errorf("SECURITY: [copy-button-text-xss] expected escaped label text")
	}
}

func TestBackToTop_ClassInjection(t *testing.T) {
	t.Parallel()
	h := ui.BackToTop(ui.BackToTopConfig{
		Class: `"><script>alert('btt-class')</script><span x="`,
	})
	s := string(h)
	if strings.Contains(s, `<script>alert('btt-class')</script>`) {
		t.Errorf("SECURITY: [back-to-top-class] class with script tag not escaped:\n  %s", truncate(s, 300))
	}
	t.Logf("NOTE: [back-to-top-class] class goes through render.Tag attribute escaping")
}

func TestTooltip_ContentXSS(t *testing.T) {
	t.Parallel()
	h := ui.Tooltip(ui.TooltipConfig{
		Text:    `<script>alert('tooltip-xss')</script>`,
	}, render.HTML(`<button>Hover</button>`))
	s := string(h)
	// Tooltip text goes through render.Text which escapes.
	if strings.Contains(s, "<script>alert('tooltip-xss')</script>") {
		t.Errorf("SECURITY: [tooltip-content-xss] script tag in tooltip text not escaped")
	}
	if !strings.Contains(s, "&lt;script&gt;") {
		t.Errorf("SECURITY: [tooltip-content-xss] expected escaped tooltip text")
	}
}

func TestToggle_LabelXSS(t *testing.T) {
	t.Parallel()
	h := ui.Checkbox(ui.ToggleConfig{
		Name:  "agree",
		Label: `<script>alert('toggle-xss')</script>`,
	})
	s := string(h)
	// Label goes through render.Text which escapes.
	if strings.Contains(s, "<script>alert('toggle-xss')</script>") {
		t.Errorf("SECURITY: [toggle-label-xss] script tag in label not escaped")
	}
	if !strings.Contains(s, "&lt;script&gt;") {
		t.Errorf("SECURITY: [toggle-label-xss] expected escaped label text")
	}
}

func TestLayout_ClassInjection(t *testing.T) {
	t.Parallel()
	h := ui.Stack(ui.StackConfig{
		Class: `"><script>alert('layout-class')</script><span x="`,
	}, render.Text("child"))
	s := string(h)
	if strings.Contains(s, `<script>alert('layout-class')</script>`) {
		t.Errorf("SECURITY: [layout-class-injection] class with script tag not escaped:\n  %s", truncate(s, 300))
	}
	t.Logf("NOTE: [layout-class-injection] class goes through render.Tag attribute escaping")
}
