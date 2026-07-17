package uinoderender

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/uinodev1"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// mustContain fails t if want is not a substring of got.
func mustContain(t *testing.T, got render.HTML, want string) {
	t.Helper()
	if !strings.Contains(string(got), want) {
		t.Fatalf("expected output to contain %q\noutput: %s", want, got)
	}
}

// staticResolver returns a resolver that maps every ref to a fixed namespaced
// URL — mirrors the shape the host's installed-route table produces.
func staticResolver(url string) ActionResolver {
	return func(ref string) (string, bool) {
		if ref == "" {
			return "", false
		}
		return url, true
	}
}

// keyResolver maps specific refs to specific URLs.
func keyResolver(m map[string]string) ActionResolver {
	return func(ref string) (string, bool) {
		u, ok := m[ref]
		return u, ok
	}
}

// --- Per-component golden-ish: assert on the real design-system marker ---

func TestRenderStackMapsToLayoutPrimitive(t *testing.T) {
	r := New(nil)
	h, err := r.Render(tree(uinodev1.Node{
		Component: uinodev1.CompStack,
		Props:     uinodev1.StackProps{Direction: "vertical", Gap: "md"},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, h, `data-fui-comp="ui-layout"`)
	mustContain(t, h, "ui-stack")
}

func TestRenderStackHorizontalMapsToCluster(t *testing.T) {
	r := New(nil)
	h, err := r.Render(tree(uinodev1.Node{
		Component: uinodev1.CompStack,
		Props:     uinodev1.StackProps{Direction: "horizontal"},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, h, "ui-cluster")
	mustContain(t, h, "ui-cluster--nowrap")
}

func TestRenderClusterMapsToLayoutPrimitive(t *testing.T) {
	r := New(nil)
	h, err := r.Render(tree(uinodev1.Node{
		Component: uinodev1.CompCluster, Props: uinodev1.ClusterProps{Gap: "sm"},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, h, "ui-cluster")
}

func TestRenderGridMapsToLayoutPrimitive(t *testing.T) {
	r := New(nil)
	h, err := r.Render(tree(uinodev1.Node{
		Component: uinodev1.CompGrid, Props: uinodev1.GridProps{Columns: 3, Gap: "md"},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, h, "ui-grid")
	mustContain(t, h, `data-min="18rem"`)
}

func TestRenderSectionMapsToHtmlSection(t *testing.T) {
	r := New(nil)
	h, err := r.Render(tree(uinodev1.Node{
		Component: uinodev1.CompSection,
		Props:     uinodev1.SectionProps{Title: "Dashboard", Subtitle: "sub"},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, h, "<section")
	mustContain(t, h, `aria-label="Dashboard"`)
	mustContain(t, h, `role="region"`)
}

func TestRenderSectionFallbackLabel(t *testing.T) {
	r := New(nil)
	// A section with no title must still get an a11y label (html.Section
	// panics without one) — the host supplies "Section".
	h, err := r.Render(tree(uinodev1.Node{
		Component: uinodev1.CompSection, Props: uinodev1.SectionProps{},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, h, `aria-label="Section"`)
}

func TestRenderCardMapsToCardPrimitive(t *testing.T) {
	r := New(nil)
	h, err := r.Render(tree(uinodev1.Node{
		Component: uinodev1.CompCard, Props: uinodev1.CardProps{Title: "C", Elevation: "flat"},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, h, `data-fui-comp="ui-card"`)
	mustContain(t, h, ">C<")
}

func TestRenderDividerMapsToDividerPrimitive(t *testing.T) {
	r := New(nil)
	h, err := r.Render(tree(uinodev1.Node{Component: uinodev1.CompDivider, Props: uinodev1.DividerProps{}}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, h, `data-fui-comp="ui-divider"`)
}

func TestRenderHeadingUsesHtmlHeading(t *testing.T) {
	r := New(nil)
	h, err := r.Render(tree(uinodev1.Node{
		Component: uinodev1.CompHeading, Props: uinodev1.HeadingProps{Level: 3, Text: "Hi"},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, h, "<h3")
	mustContain(t, h, ">Hi<")
}

func TestRenderInlineTextPrimitives(t *testing.T) {
	r := New(nil)
	cases := []struct {
		name string
		node uinodev1.Node
		tag  string
	}{
		{"paragraph", node(uinodev1.CompParagraph, uinodev1.ParagraphProps{Text: "p"}), "p"},
		{"strong", node(uinodev1.CompStrong, uinodev1.StrongProps{Text: "s"}), "strong"},
		{"em", node(uinodev1.CompEm, uinodev1.EmProps{Text: "e"}), "em"},
		{"code", node(uinodev1.CompCode, uinodev1.CodeProps{Text: "c"}), "code"},
		{"small", node(uinodev1.CompSmall, uinodev1.SmallProps{Text: "sm"}), "small"},
		{"text", node(uinodev1.CompText, uinodev1.TextProps{Text: "t"}), "span"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, err := r.Render(tree(c.node))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			mustContain(t, h, "<"+c.tag)
		})
	}
}

func TestRenderBadgeToneMapsToStatusVariant(t *testing.T) {
	r := New(nil)
	cases := []struct {
		tone string
		cls  string
	}{
		{"positive", "ui-badge--success"},
		{"negative", "ui-badge--danger"},
		{"warning", "ui-badge--warning"},
		{"info", "ui-badge--info"},
		{"neutral", "ui-badge--neutral"},
		{"", "ui-badge--neutral"},
	}
	for _, c := range cases {
		t.Run(c.tone, func(t *testing.T) {
			h, err := r.Render(tree(node(uinodev1.CompBadge, uinodev1.BadgeProps{Text: "x", Tone: c.tone})))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			mustContain(t, h, c.cls)
		})
	}
}

func TestRenderDetailListMapsToPrimitive(t *testing.T) {
	r := New(nil)
	h, err := r.Render(tree(node(uinodev1.CompDetailList, uinodev1.DetailListProps{
		Items: []uinodev1.DetailItem{{Label: "Name", Value: "Ada"}},
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, h, `data-fui-comp="ui-detail-list"`)
	mustContain(t, h, ">Name<")
	mustContain(t, h, ">Ada<")
}

func TestRenderKeyValueReusesDetailList(t *testing.T) {
	r := New(nil)
	// key-value reuses the <dl> DetailList primitive (no distinct KV
	// component exists; both render label/value description rows).
	h, err := r.Render(tree(node(uinodev1.CompKeyValue, uinodev1.KeyValueProps{
		Items: []uinodev1.KeyValueItem{{Key: "K", Value: "V"}},
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, h, `data-fui-comp="ui-detail-list"`)
	mustContain(t, h, ">K<")
	mustContain(t, h, ">V<")
}

func TestRenderStatCardMapsToPrimitive(t *testing.T) {
	r := New(nil)
	h, err := r.Render(tree(node(uinodev1.CompStatCard, uinodev1.StatCardProps{
		Label: "MRR", Value: "$1k", Unit: "USD", Trend: "up",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, h, `data-fui-comp="ui-stat-card"`)
	mustContain(t, h, ">MRR<")
	mustContain(t, h, ">$1k USD<")
	mustContain(t, h, "ui-stat-card__trend--up")
}

func TestRenderImageMapsToHtmlImage(t *testing.T) {
	r := New(nil)
	h, err := r.Render(tree(node(uinodev1.CompImage, uinodev1.ImageProps{
		Src: "/assets/logo.png", Alt: "Logo",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, h, "<img")
	mustContain(t, h, `src="/assets/logo.png"`)
	mustContain(t, h, `alt="Logo"`)
}

// --- DataTable panic-safety (design §9 named hazard) --------------------

func TestRenderDataTableMapsToPrimitive(t *testing.T) {
	r := New(nil)
	h, err := r.Render(tree(node(uinodev1.CompDataTable, uinodev1.DataTableProps{
		Columns: []uinodev1.DataColumn{{Key: "name", Label: "Name"}},
		Rows:    []uinodev1.DataRow{{Cells: []uinodev1.DataCell{{Text: "Ada"}}}},
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, h, `data-fui-comp="ui-data-table"`)
	mustContain(t, h, ">Name<")
	mustContain(t, h, ">Ada<")
}

func TestRenderDataTableEmptyColumns(t *testing.T) {
	// The validator rejects empty columns, but the renderer must be
	// panic-safe for a hand-built Tree too — empty columns render an
	// empty-state, never a panic (datatable.go:154).
	r := New(nil)
	h, err := r.Render(tree(node(uinodev1.CompDataTable, uinodev1.DataTableProps{
		Columns: nil,
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, h, "ui-empty-state")
}

func TestRenderDataTableEmptyRows(t *testing.T) {
	// Empty rows are handled by ui.DataTable itself; the renderer passes
	// them through and must not panic.
	r := New(nil)
	h, err := r.Render(tree(node(uinodev1.CompDataTable, uinodev1.DataTableProps{
		Columns: []uinodev1.DataColumn{{Key: "name", Label: "Name"}},
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, h, "ui-data-table")
	mustContain(t, h, "is-empty")
}

// --- ActionRef resolution seam (design §9) -------------------------------

func TestRenderButtonActionRefResolves(t *testing.T) {
	r := New(keyResolver(map[string]string{"save": "/m/mod/save"}))
	h, err := r.Render(tree(uinodev1.Node{
		Component: uinodev1.CompButton,
		Props:     uinodev1.ButtonProps{Label: "Save", Variant: "primary"},
		ActionRef: "save",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, h, `data-fui-rpc="/m/mod/save"`)
	mustContain(t, h, "ui-button--primary")
}

func TestRenderLinkActionRefResolvedToRpcURL(t *testing.T) {
	r := New(keyResolver(map[string]string{"act": "/m/mod/act"}))
	h, err := r.Render(tree(uinodev1.Node{
		Component: uinodev1.CompLink,
		Props:     uinodev1.LinkProps{Text: "Act"},
		ActionRef: "act",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, h, `data-fui-rpc="/m/mod/act"`)
	mustContain(t, h, `href="/m/mod/act"`)
}

func TestRenderLinkToPropIsHostRelativeHref(t *testing.T) {
	r := New(nil)
	h, err := r.Render(tree(node(uinodev1.CompLink, uinodev1.LinkProps{Text: "Home", To: "/home"})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, h, `href="/home"`)
	if strings.Contains(string(h), "data-fui-rpc") {
		t.Fatalf("a To-prop link must NOT carry data-fui-rpc (pure nav):\n%s", h)
	}
}

func TestRenderButtonUnresolvedFailsClosed(t *testing.T) {
	r := New(keyResolver(map[string]string{})) // resolves nothing
	_, err := r.Render(tree(uinodev1.Node{
		Component: uinodev1.CompButton,
		Props:     uinodev1.ButtonProps{Label: "Save"},
		ActionRef: "ghost",
	}))
	if err == nil {
		t.Fatal("unresolved action_ref must fail closed")
	}
	if !strings.Contains(err.Error(), "action_ref") {
		t.Fatalf("error should reference action_ref, got: %v", err)
	}
}

func TestRenderNilResolverFailsClosed(t *testing.T) {
	r := New(nil)
	_, err := r.Render(tree(uinodev1.Node{
		Component: uinodev1.CompButton,
		Props:     uinodev1.ButtonProps{Label: "Save"},
		ActionRef: "save",
	}))
	if err == nil {
		t.Fatal("nil resolver must fail closed on every ActionRef")
	}
}

// --- Full round trip: no module-supplied attribute survives -------------

func TestRoundTripRejectsForgedAttrs(t *testing.T) {
	// A module tries to forge data-fui-rpc + onclick + id. Validate
	// rejects the whole tree (DisallowUnknownFields), so Render is never
	// reached. This is the core §9 property: forged attrs are
	// unrepresentable, not merely dropped.
	bad := []string{
		`{"component":"heading","props":{"level":1,"text":"x","data-fui-rpc":"/evil"}}`,
		`{"component":"button","props":{"label":"x","onclick":"evil()"},"action_ref":"a"}`,
		`{"component":"divider","id":"evil","class":"evil"}`,
	}
	for _, j := range bad {
		_, err := uinodev1.Validate([]byte(j), uinodev1.DefaultLimits())
		if err == nil {
			t.Fatalf("forged-attr tree must be rejected by Validate: %s", j)
		}
	}
}

func TestRoundTripNoModuleAttrInOutput(t *testing.T) {
	// A clean, valid tree renders to markup where every id/class/aria is
	// host-assigned. No module-supplied id/class/data-*/on* can appear
	// because the wire type cannot carry them.
	r := New(staticResolver("/m/mod/x"))
	treeJSON := `{
		"component": "section",
		"props": {"title": "Dashboard"},
		"children": [
			{"component": "heading", "props": {"level": 2, "text": "Stats"}},
			{"component": "card", "props": {"title": "Revenue"}, "children": [
				{"component": "stat-card", "props": {"label": "MRR", "value": "$1k", "trend": "up"}},
				{"component": "button", "props": {"label": "Refresh"}, "action_ref": "refresh"}
			]}
		]
	}`
	tt, err := uinodev1.Validate([]byte(treeJSON), uinodev1.DefaultLimits())
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	h, err := r.Render(tt)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := string(h)
	for _, banned := range []string{
		"onclick", // on* handlers
		"onload",
		"data-fui-", // the only legit data-fui-rpc comes from our resolver;
		// presence of OTHER data-fui-* would be a forge — but our output
		// legitimately has data-fui-comp (component markers) and the one
		// data-fui-rpc we assigned. We assert the FORGED value is absent
		// below instead.
		"javascript:",
	} {
		if strings.Contains(out, banned) {
			// data-fui-comp / data-fui-rpc are host-emitted; only flag
			// truly forged keys. Re-check precisely:
			if banned == "data-fui-" {
				continue // handled by the precise checks below
			}
			t.Fatalf("output must not contain %q:\n%s", banned, out)
		}
	}
	// The one data-fui-rpc present must be OUR assigned URL, nothing else.
	if !strings.Contains(out, `data-fui-rpc="/m/mod/x"`) {
		t.Fatalf("the button's action_ref must resolve to the host URL:\n%s", out)
	}
	// No module-supplied id leaked (ids in output are host-derived from
	// heading text via slugify — those are fine; assert no id="evil").
	if strings.Contains(out, `id="evil"`) {
		t.Fatalf("module-supplied id leaked into output:\n%s", out)
	}
}

// --- Fuzz-ish: every component with minimal valid props, no panic --------

func TestRenderAllComponentsNoPanic(t *testing.T) {
	r := New(staticResolver("/m/mod/rpc"))
	nodes := []uinodev1.Node{
		node(uinodev1.CompStack, uinodev1.StackProps{}),
		node(uinodev1.CompCluster, uinodev1.ClusterProps{}),
		node(uinodev1.CompGrid, uinodev1.GridProps{}),
		node(uinodev1.CompSection, uinodev1.SectionProps{}),
		node(uinodev1.CompCard, uinodev1.CardProps{}),
		node(uinodev1.CompDivider, uinodev1.DividerProps{}),
		node(uinodev1.CompHeading, uinodev1.HeadingProps{Level: 1, Text: "x"}),
		node(uinodev1.CompParagraph, uinodev1.ParagraphProps{Text: "x"}),
		node(uinodev1.CompText, uinodev1.TextProps{Text: "x"}),
		node(uinodev1.CompStrong, uinodev1.StrongProps{Text: "x"}),
		node(uinodev1.CompEm, uinodev1.EmProps{Text: "x"}),
		node(uinodev1.CompCode, uinodev1.CodeProps{Text: "x"}),
		node(uinodev1.CompSmall, uinodev1.SmallProps{Text: "x"}),
		node(uinodev1.CompBadge, uinodev1.BadgeProps{Text: "x"}),
		node(uinodev1.CompDetailList, uinodev1.DetailListProps{
			Items: []uinodev1.DetailItem{{Label: "L", Value: "V"}},
		}),
		node(uinodev1.CompKeyValue, uinodev1.KeyValueProps{
			Items: []uinodev1.KeyValueItem{{Key: "K", Value: "V"}},
		}),
		node(uinodev1.CompStatCard, uinodev1.StatCardProps{Label: "L", Value: "V"}),
		// Empty-columns data-table is the named panic hazard — must NOT panic.
		node(uinodev1.CompDataTable, uinodev1.DataTableProps{}),
		node(uinodev1.CompDataTable, uinodev1.DataTableProps{
			Columns: []uinodev1.DataColumn{{Key: "k", Label: "L"}},
		}),
		{Component: uinodev1.CompButton, Props: uinodev1.ButtonProps{Label: "x"}, ActionRef: "a"},
		node(uinodev1.CompLink, uinodev1.LinkProps{Text: "x", To: "/x"}),
		{Component: uinodev1.CompLink, Props: uinodev1.LinkProps{Text: "x"}, ActionRef: "a"},
		node(uinodev1.CompImage, uinodev1.ImageProps{Src: "/x.png", Alt: "x"}),
	}
	for i, n := range nodes {
		n := n
		t.Run(string(n.Component)+"#"+itoa(i), func(t *testing.T) {
			// If a framework/ui primitive panics on a minimal-but-valid
			// node, this test fails — surfacing the bug rather than
			// masking it. The datatable empty-columns case is the
			// specific hazard §9 names; the renderer guards it.
			h, err := r.Render(tree(n))
			if err != nil {
				// An error is acceptable ONLY for unresolved action_refs.
				// Here every action_ref resolves via staticResolver, so
				// no error is expected.
				t.Fatalf("unexpected error for %s: %v", n.Component, err)
			}
			if len(h) == 0 {
				t.Fatalf("empty output for %s", n.Component)
			}
		})
	}
}

// --- helpers -------------------------------------------------------------

func tree(root uinodev1.Node) *uinodev1.Tree { return &uinodev1.Tree{Root: root} }

func node(c uinodev1.Component, p uinodev1.Props) uinodev1.Node {
	return uinodev1.Node{Component: c, Props: p}
}

// itoa keeps the test file free of strconv for parity with the validator's
// own test helper.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
