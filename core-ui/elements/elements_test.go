package elements

import (
	"strings"
	"testing"

	"github.com/gofastr/gofastr/core/render"
)

// ============================================================================
// Helper: assert that HTML contains a substring.
// ============================================================================
func assertContains(t *testing.T, got render.HTML, substr string) {
	t.Helper()
	if !strings.Contains(string(got), substr) {
		t.Errorf("expected HTML to contain %q, got %q", substr, got)
	}
}

func assertNotContains(t *testing.T, got render.HTML, substr string) {
	t.Helper()
	if strings.Contains(string(got), substr) {
		t.Errorf("expected HTML NOT to contain %q, got %q", substr, got)
	}
}

// ============================================================================
// attrs.go — Helper functions
// ============================================================================

func TestMergeAttrs(t *testing.T) {
	tests := []struct {
		name   string
		inputs []Attrs
		want   Attrs
	}{
		{"nil input", nil, nil},
		{"single map", []Attrs{{"a": "1"}}, Attrs{"a": "1"}},
		{"merge two", []Attrs{{"a": "1"}, {"b": "2"}}, Attrs{"a": "1", "b": "2"}},
		{"overwrite", []Attrs{{"a": "1"}, {"a": "2"}}, Attrs{"a": "2"}},
		{"nil in list", []Attrs{nil, {"a": "1"}}, Attrs{"a": "1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MergeAttrs(tt.inputs...)
			if tt.want == nil && got != nil {
				t.Fatalf("expected nil, got %v", got)
			}
			if tt.want != nil {
				for k, v := range tt.want {
					if got[k] != v {
						t.Errorf("MergeAttrs()[%q] = %q, want %q", k, got[k], v)
					}
				}
			}
		})
	}
}

func TestClasses(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]bool
		want  string // expected class string (order may vary)
	}{
		{"empty", map[string]bool{}, ""},
		{"all false", map[string]bool{"a": false, "b": false}, ""},
		{"single true", map[string]bool{"active": true}, "active"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classes(tt.input)
			if tt.want == "" {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			if got == nil || got["class"] != tt.want {
				t.Errorf("Classes() = %v, want class=%q", got, tt.want)
			}
		})
	}

	// Test that mixed produces a class string containing both true values.
	got := Classes(map[string]bool{"active": true, "big": true, "skip": false})
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	cls := got["class"]
	if !strings.Contains(cls, "active") || !strings.Contains(cls, "big") {
		t.Errorf("expected class to contain 'active' and 'big', got %q", cls)
	}
	if strings.Contains(cls, "skip") {
		t.Errorf("expected class NOT to contain 'skip', got %q", cls)
	}
}

func TestDataAttrs(t *testing.T) {
	got := DataAttrs(map[string]string{"id": "123", "role": "admin"})
	if got["data-id"] != "123" {
		t.Errorf("data-id = %q, want %q", got["data-id"], "123")
	}
	if got["data-role"] != "admin" {
		t.Errorf("data-role = %q, want %q", got["data-role"], "admin")
	}

	got2 := DataAttrs(nil)
	if got2 != nil {
		t.Errorf("expected nil for nil input, got %v", got2)
	}
}

func TestID(t *testing.T) {
	got := ID("my-element")
	if got["id"] != "my-element" {
		t.Errorf("ID() = %v, want id=my-element", got)
	}
}

func TestAria(t *testing.T) {
	got := Aria("label", "Close dialog")
	if got["aria-label"] != "Close dialog" {
		t.Errorf("Aria() = %v, want aria-label=Close dialog", got)
	}
}

// ============================================================================
// roles.go — Constants
// ============================================================================

func TestRoleConstants(t *testing.T) {
	roles := map[string]string{
		"RoleBanner":        RoleBanner,
		"RoleNavigation":    RoleNavigation,
		"RoleMain":          RoleMain,
		"RoleContentinfo":   RoleContentinfo,
		"RoleComplementary": RoleComplementary,
		"RoleSearch":        RoleSearch,
		"RoleForm":          RoleForm,
		"RoleRegion":        RoleRegion,
		"RoleDialog":        RoleDialog,
		"RoleAlert":         RoleAlert,
		"RoleAlertDialog":   RoleAlertDialog,
		"RoleStatus":        RoleStatus,
		"RoleLog":           RoleLog,
		"RoleMarquee":       RoleMarquee,
		"RoleTimer":         RoleTimer,
		"RoleButton":        RoleButton,
		"RoleLink":          RoleLink,
		"RoleCheckbox":      RoleCheckbox,
		"RoleRadio":         RoleRadio,
		"RoleTab":           RoleTab,
		"RoleTabList":       RoleTabList,
		"RoleTabPanel":      RoleTabPanel,
		"RoleGrid":          RoleGrid,
		"RoleGridCell":      RoleGridCell,
		"RoleRow":           RoleRow,
		"RoleRowGroup":      RoleRowGroup,
		"RoleTable":         RoleTable,
		"RoleList":          RoleList,
		"RoleListItem":      RoleListItem,
		"RoleListbox":       RoleListbox,
		"RoleOption":        RoleOption,
		"RoleMenu":          RoleMenu,
		"RoleMenuItem":      RoleMenuItem,
	}
	for name, val := range roles {
		if val == "" {
			t.Errorf("role constant %s is empty", name)
		}
	}
}

// ============================================================================
// text.go — Text elements
// ============================================================================

func TestHeading(t *testing.T) {
	t.Run("h1 through h6", func(t *testing.T) {
		for level := 1; level <= 6; level++ {
			h := Heading(level, nil, render.Text("Title"))
			tag := string(h)[1:3]
			if level >= 1 && level <= 9 {
				expected := []byte{'h', byte('0' + level)}
				if tag != string(expected) {
					t.Errorf("Heading(%d) produced tag %q, want <h%d>", level, tag, level)
				}
			}
			assertContains(t, h, "Title")
			assertContains(t, h, "</h")
		}
	})

	t.Run("auto-generates id", func(t *testing.T) {
		h := Heading(2, nil, render.Text("Hello World"))
		if !strings.Contains(string(h), `id="heading-hello-world"`) {
			t.Errorf("expected auto-generated id, got %q", h)
		}
	})

	t.Run("preserves explicit id", func(t *testing.T) {
		h := Heading(1, Attrs{"id": "custom-id"}, render.Text("Title"))
		assertContains(t, h, `id="custom-id"`)
		assertNotContains(t, h, `id="heading-`)
	})

	t.Run("clamps level", func(t *testing.T) {
		h := Heading(0, nil, render.Text("Low"))
		assertContains(t, h, "<h1")
		h = Heading(10, nil, render.Text("High"))
		assertContains(t, h, "<h6")
	})

	t.Run("nil children", func(t *testing.T) {
		h := Heading(3, nil)
		assertContains(t, h, "<h3")
		assertContains(t, h, "</h3>")
	})
}

func TestParagraph(t *testing.T) {
	p := Paragraph(nil, render.Text("Hello"))
	assertContains(t, p, "<p>")
	assertContains(t, p, "Hello")
	assertContains(t, p, "</p>")

	p2 := Paragraph(Attrs{"class": "lead"}, render.Text("Intro"))
	assertContains(t, p2, `class="lead"`)
}

func TestSpan(t *testing.T) {
	s := Span(nil, render.Text("inline"))
	assertContains(t, s, "<span>")
	assertContains(t, s, "inline")
}

func TestStrong(t *testing.T) {
	s := Strong(nil, render.Text("bold"))
	assertContains(t, s, "<strong>")
	assertContains(t, s, "bold")
}

func TestEm(t *testing.T) {
	e := Em(nil, render.Text("italic"))
	assertContains(t, e, "<em>")
	assertContains(t, e, "italic")
}

func TestCode(t *testing.T) {
	c := Code(nil, render.Text("x := 1"))
	assertContains(t, c, "<code>")
	assertContains(t, c, "x := 1")
}

func TestPre(t *testing.T) {
	p := Pre(nil, render.Text("  indented  "))
	assertContains(t, p, "<pre>")
	assertContains(t, p, "  indented  ")
}

func TestBlockquote(t *testing.T) {
	b := Blockquote(Attrs{"cite": "https://example.com"}, render.Text("Quote"))
	assertContains(t, b, "<blockquote")
	assertContains(t, b, "Quote")
	assertContains(t, b, `cite="https://example.com"`)
}

func TestCite(t *testing.T) {
	c := Cite(nil, render.Text("Work Title"))
	assertContains(t, c, "<cite>")
	assertContains(t, c, "Work Title")
}

func TestSmall(t *testing.T) {
	s := Small(nil, render.Text("fine print"))
	assertContains(t, s, "<small>")
}

func TestMark(t *testing.T) {
	m := Mark(nil, render.Text("highlighted"))
	assertContains(t, m, "<mark>")
}

func TestAbbr(t *testing.T) {
	a := Abbr("HyperText Markup Language", nil, render.Text("HTML"))
	assertContains(t, a, "<abbr")
	assertContains(t, a, `title="HyperText Markup Language"`)
	assertContains(t, a, "HTML")
}

func TestTime(t *testing.T) {
	tm := Time("2024-01-15", nil, render.Text("Jan 15"))
	assertContains(t, tm, "<time")
	assertContains(t, tm, `datetime="2024-01-15"`)
	assertContains(t, tm, "Jan 15")
}

// ============================================================================
// structure.go — Structural elements
// ============================================================================

func TestDiv(t *testing.T) {
	d := Div(nil, render.Text("content"))
	assertContains(t, d, "<div>")
	assertContains(t, d, "content")
}

func TestArticle(t *testing.T) {
	a := Article(nil, render.Text("post"))
	assertContains(t, a, "<article>")
}

func TestSection(t *testing.T) {
	t.Run("no role without aria-label", func(t *testing.T) {
		s := Section(nil, render.Text("content"))
		assertContains(t, s, "<section>")
		assertNotContains(t, s, "role=")
	})

	t.Run("adds role=region with aria-label", func(t *testing.T) {
		s := Section(Attrs{"aria-label": "Features"}, render.Text("content"))
		assertContains(t, s, `role="region"`)
	})

	t.Run("adds role=region with aria-labelledby", func(t *testing.T) {
		s := Section(Attrs{"aria-labelledby": "heading-1"}, render.Text("content"))
		assertContains(t, s, `role="region"`)
	})
}

func TestMain(t *testing.T) {
	m := Main(nil, render.Text("main content"))
	assertContains(t, m, "<main")
	assertContains(t, m, `role="main"`)
	assertContains(t, m, `id="main-content"`)

	t.Run("preserves explicit id", func(t *testing.T) {
		m := Main(Attrs{"id": "content"})
		assertContains(t, m, `id="content"`)
	})
}

func TestHeader(t *testing.T) {
	h := Header(nil, render.Text("header"))
	assertContains(t, h, "<header")
	assertContains(t, h, `role="banner"`)
}

func TestFooter(t *testing.T) {
	f := Footer(nil, render.Text("footer"))
	assertContains(t, f, "<footer")
	assertContains(t, f, `role="contentinfo"`)
}

func TestNav(t *testing.T) {
	n := Nav(Attrs{"aria-label": "Main nav"}, render.Text("links"))
	assertContains(t, n, "<nav")
	assertContains(t, n, `role="navigation"`)
	assertContains(t, n, `aria-label="Main nav"`)
}

func TestAside(t *testing.T) {
	a := Aside(nil, render.Text("sidebar"))
	assertContains(t, a, "<aside")
	assertContains(t, a, `role="complementary"`)
}

func TestFigure(t *testing.T) {
	f := Figure(nil, render.Text("fig content"))
	assertContains(t, f, "<figure>")
}

func TestFigCaption(t *testing.T) {
	f := FigCaption(nil, render.Text("caption"))
	assertContains(t, f, "<figcaption>")
}

func TestDetails(t *testing.T) {
	d := Details(nil, render.Text("disclosed content"))
	assertContains(t, d, "<details>")
}

func TestSummary(t *testing.T) {
	s := Summary(nil, render.Text("click to expand"))
	assertContains(t, s, "<summary>")
}

// ============================================================================
// interactive.go — Interactive elements
// ============================================================================

func TestButton(t *testing.T) {
	t.Run("with label", func(t *testing.T) {
		b := Button("Submit", nil)
		assertContains(t, b, "<button")
		assertContains(t, b, `type="button"`)
		assertContains(t, b, `aria-label="Submit"`)
		assertContains(t, b, "Submit")
	})

	t.Run("empty label no aria-label", func(t *testing.T) {
		b := Button("", Attrs{"class": "icon-btn"})
		assertNotContains(t, b, "aria-label=")
		assertContains(t, b, `type="button"`)
	})

	t.Run("respects existing type", func(t *testing.T) {
		b := Button("Go", Attrs{"type": "submit"})
		assertContains(t, b, `type="submit"`)
	})
}

func TestLink(t *testing.T) {
	a := Link("/about", "About Us", nil)
	assertContains(t, a, "<a")
	assertContains(t, a, `href="/about"`)
	assertContains(t, a, "About Us")
}

func TestForm(t *testing.T) {
	f := Form("POST", "/submit", nil, render.Text("form content"))
	assertContains(t, f, "<form")
	assertContains(t, f, `method="POST"`)
	assertContains(t, f, `action="/submit"`)
}

func TestFormEmptyAction(t *testing.T) {
	f := Form("GET", "", nil)
	assertContains(t, f, `method="GET"`)
	assertNotContains(t, f, "action=")
}

func TestInput(t *testing.T) {
	inp := Input("text", "email", Attrs{"id": "email-field"})
	assertContains(t, inp, "<input")
	assertContains(t, inp, `type="text"`)
	assertContains(t, inp, `name="email"`)
	assertContains(t, inp, `id="email-field"`)
	// Void element: no closing tag.
	assertNotContains(t, inp, "</input>")
}

func TestLabel(t *testing.T) {
	l := Label("email-field", "Email Address", nil)
	assertContains(t, l, "<label")
	assertContains(t, l, `for="email-field"`)
	assertContains(t, l, "Email Address")
}

func TestSelect(t *testing.T) {
	opts := []SelectOption{
		{Value: "us", Text: "United States", Selected: false},
		{Value: "ca", Text: "Canada", Selected: true},
	}
	s := Select("country", opts, nil)
	assertContains(t, s, "<select")
	assertContains(t, s, `name="country"`)
	assertContains(t, s, `value="us"`)
	assertContains(t, s, "United States")
	assertContains(t, s, `value="ca"`)
	assertContains(t, s, "Canada")
	assertContains(t, s, `selected="selected"`)
}

func TestOption(t *testing.T) {
	o := Option("val", "Label", true)
	assertContains(t, o, "<option")
	assertContains(t, o, `value="val"`)
	assertContains(t, o, `selected="selected"`)
	assertContains(t, o, "Label")

	o2 := Option("val2", "Label2", false)
	assertNotContains(t, o2, "selected")
}

func TestTextArea(t *testing.T) {
	ta := TextArea("description", Attrs{"rows": "5"})
	assertContains(t, ta, "<textarea")
	assertContains(t, ta, `name="description"`)
	assertContains(t, ta, `rows="5"`)
	assertContains(t, ta, "</textarea>")
}

func TestFieldSet(t *testing.T) {
	fs := FieldSet("Personal Info", nil, render.Text("fields here"))
	assertContains(t, fs, "<fieldset")
	assertContains(t, fs, "<legend>")
	assertContains(t, fs, "Personal Info")
	assertContains(t, fs, "fields here")
}

func TestLegend(t *testing.T) {
	l := Legend(nil, render.Text("Group Label"))
	assertContains(t, l, "<legend>")
	assertContains(t, l, "Group Label")
}

// ============================================================================
// lists.go — List elements
// ============================================================================

func TestOrderedList(t *testing.T) {
	ol := OrderedList(nil, render.Text("item"))
	assertContains(t, ol, "<ol")
	assertContains(t, ol, `role="list"`)
}

func TestUnorderedList(t *testing.T) {
	ul := UnorderedList(nil, render.Text("item"))
	assertContains(t, ul, "<ul")
	assertContains(t, ul, `role="list"`)
}

func TestListItem(t *testing.T) {
	li := ListItem(nil, render.Text("first"))
	assertContains(t, li, "<li")
	assertContains(t, li, `role="listitem"`)
}

func TestDescriptionList(t *testing.T) {
	dl := DescriptionList(nil, render.Text("content"))
	assertContains(t, dl, "<dl>")
}

func TestDescriptionTerm(t *testing.T) {
	dt := DescriptionTerm(nil, render.Text("Term"))
	assertContains(t, dt, "<dt>")
}

func TestDescriptionDetail(t *testing.T) {
	dd := DescriptionDetail(nil, render.Text("Definition"))
	assertContains(t, dd, "<dd>")
}

// ============================================================================
// table.go — Table elements
// ============================================================================

func TestTable(t *testing.T) {
	tbl := Table(nil, render.Text("table content"))
	assertContains(t, tbl, "<table")
	assertContains(t, tbl, `role="table"`)
}

func TestCaption(t *testing.T) {
	c := Caption(nil, render.Text("Q1 Sales"))
	assertContains(t, c, "<caption>")
	assertContains(t, c, "Q1 Sales")
}

func TestThead(t *testing.T) {
	th := Thead(nil, render.Text("head"))
	assertContains(t, th, "<thead")
	assertContains(t, th, `role="rowgroup"`)
}

func TestTbody(t *testing.T) {
	tb := Tbody(nil, render.Text("body"))
	assertContains(t, tb, "<tbody")
	assertContains(t, tb, `role="rowgroup"`)
}

func TestTfoot(t *testing.T) {
	tf := Tfoot(nil, render.Text("foot"))
	assertContains(t, tf, "<tfoot")
	assertContains(t, tf, `role="rowgroup"`)
}

func TestTableRow(t *testing.T) {
	tr := TableRow(nil, render.Text("row"))
	assertContains(t, tr, "<tr")
	assertContains(t, tr, `role="row"`)
}

func TestTH(t *testing.T) {
	t.Run("default scope=col", func(t *testing.T) {
		th := TH(nil, render.Text("Name"))
		assertContains(t, th, "<th")
		assertContains(t, th, `scope="col"`)
		assertContains(t, th, `role="columnheader"`)
	})

	t.Run("scope=row", func(t *testing.T) {
		th := TH(Attrs{"scope": "row"}, render.Text("Total"))
		assertContains(t, th, `scope="row"`)
		assertContains(t, th, `role="rowheader"`)
	})
}

func TestTD(t *testing.T) {
	td := TD(nil, render.Text("data"))
	assertContains(t, td, "<td")
	assertContains(t, td, `role="cell"`)
}

// ============================================================================
// media.go — Media and void elements
// ============================================================================

func TestImage(t *testing.T) {
	t.Run("with alt text", func(t *testing.T) {
		img := Image("/photo.jpg", "A sunset", nil)
		assertContains(t, img, "<img")
		assertContains(t, img, `src="/photo.jpg"`)
		assertContains(t, img, `alt="A sunset"`)
		assertNotContains(t, img, "role=")
		assertNotContains(t, img, "</img>")
	})

	t.Run("decorative (empty alt)", func(t *testing.T) {
		img := Image("/decor.png", "", nil)
		assertContains(t, img, `alt=""`)
		assertContains(t, img, `role="presentation"`)
	})
}

func TestAudio(t *testing.T) {
	a := Audio(Attrs{"controls": "controls"}, render.Text("fallback"))
	assertContains(t, a, "<audio")
	assertContains(t, a, `controls="controls"`)
	assertContains(t, a, "fallback")
}

func TestVideo(t *testing.T) {
	v := Video(Attrs{"controls": "controls"}, render.Text("fallback"))
	assertContains(t, v, "<video")
	assertContains(t, v, `controls="controls"`)
}

func TestSource(t *testing.T) {
	s := Source("movie.mp4", "video/mp4", nil)
	assertContains(t, s, "<source")
	assertContains(t, s, `src="movie.mp4"`)
	assertContains(t, s, `type="video/mp4"`)
	assertNotContains(t, s, "</source>")
}

func TestHR(t *testing.T) {
	h := HR(nil)
	assertContains(t, h, "<hr>")
	assertNotContains(t, h, "</hr>")
}

func TestBR(t *testing.T) {
	b := BR(nil)
	assertContains(t, b, "<br>")
	assertNotContains(t, b, "</br>")
}

func TestMeta(t *testing.T) {
	m := Meta("description", "A great site")
	assertContains(t, m, "<meta")
	assertContains(t, m, `name="description"`)
	assertContains(t, m, `content="A great site"`)
}

func TestStyleSheet(t *testing.T) {
	s := StyleSheet("/css/app.css")
	assertContains(t, s, "<link")
	assertContains(t, s, `rel="stylesheet"`)
	assertContains(t, s, `href="/css/app.css"`)
}

func TestScript(t *testing.T) {
	s := Script("/js/app.js")
	assertContains(t, s, "<script")
	assertContains(t, s, `src="/js/app.js"`)
	assertContains(t, s, "</script>")
}

// ============================================================================
// Auto-escaping tests
// ============================================================================

func TestAutoEscaping(t *testing.T) {
	malicious := "<script>alert('xss')</script>"

	t.Run("Paragraph escapes content", func(t *testing.T) {
		p := Paragraph(nil, render.Text(malicious))
		assertNotContains(t, p, "<script>")
		assertContains(t, p, "&lt;script&gt;")
	})

	t.Run("Span escapes content", func(t *testing.T) {
		s := Span(nil, render.Text(malicious))
		assertNotContains(t, s, "<script>")
	})

	t.Run("Link escapes text", func(t *testing.T) {
		a := Link("/page", malicious, nil)
		assertNotContains(t, a, "<script>")
	})

	t.Run("Label escapes text", func(t *testing.T) {
		l := Label("field", malicious, nil)
		assertNotContains(t, l, "<script>")
	})
}

// ============================================================================
// Nil attrs edge case
// ============================================================================

func TestNilAttrs(t *testing.T) {
	// Ensure nil attrs doesn't panic and produces clean HTML.
	elements := []struct {
		name string
		html render.HTML
	}{
		{"Div", Div(nil)},
		{"Article", Article(nil)},
		{"Paragraph", Paragraph(nil)},
		{"Span", Span(nil)},
		{"Strong", Strong(nil)},
		{"Em", Em(nil)},
		{"Code", Code(nil)},
		{"Pre", Pre(nil)},
		{"Blockquote", Blockquote(nil)},
		{"Cite", Cite(nil)},
		{"Small", Small(nil)},
		{"Mark", Mark(nil)},
		{"Figure", Figure(nil)},
		{"FigCaption", FigCaption(nil)},
		{"Details", Details(nil)},
		{"Summary", Summary(nil)},
		{"Legend", Legend(nil)},
		{"Caption", Caption(nil)},
		{"DescriptionList", DescriptionList(nil)},
		{"DescriptionTerm", DescriptionTerm(nil)},
		{"DescriptionDetail", DescriptionDetail(nil)},
	}

	for _, el := range elements {
		t.Run(el.name, func(t *testing.T) {
			// Just ensure no panic and non-empty output.
			if el.html == "" {
				t.Errorf("%s with nil attrs produced empty HTML", el.name)
			}
		})
	}
}

// ============================================================================
// Integration: full table build
// ============================================================================

func TestFullTable(t *testing.T) {
	tbl := Table(Attrs{"class": "data-table"},
		Caption(nil, render.Text("Sales Report")),
		Thead(nil,
			TableRow(nil,
				TH(nil, render.Text("Product")),
				TH(nil, render.Text("Revenue")),
			),
		),
		Tbody(nil,
			TableRow(nil,
				TD(nil, render.Text("Widget A")),
				TD(nil, render.Text("$1,200")),
			),
		),
	)

	got := string(tbl)
	assertContains(t, tbl, `<table`)
	assertContains(t, tbl, `<caption>Sales Report</caption>`)
	assertContains(t, tbl, "<thead")
	assertContains(t, tbl, "<tbody")
	assertContains(t, tbl, "<th")
	assertContains(t, tbl, "<td")
	assertContains(t, tbl, "</table>")

	_ = got
}

// ============================================================================
// Integration: full form build
// ============================================================================

func TestFullForm(t *testing.T) {
	f := Form("POST", "/register", Attrs{"class": "form"},
		FieldSet("Account", nil,
			Label("email", "Email", nil),
			Input("email", "email", Attrs{"id": "email", "required": "required"}),
		),
	)

	assertContains(t, f, `method="POST"`)
	assertContains(t, f, `action="/register"`)
	assertContains(t, f, "<fieldset")
	assertContains(t, f, "<legend>Account</legend>")
	assertContains(t, f, `<label for="email">Email</label>`)
	assertContains(t, f, `<input`)
}
