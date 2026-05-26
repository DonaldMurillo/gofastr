package render

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Escape / XSS ----------------------------------------------------------

func TestEscape(t *testing.T) {
	tests := []struct{ in, want string }{
		{`<script>alert(1)</script>`, "&lt;script&gt;alert(1)&lt;/script&gt;"},
		{`Tom & Jerry`, "Tom &amp; Jerry"},
		{`"quoted"`, "&quot;quoted&quot;"},
		{`it's`, "it&#39;s"},
		{`a < b > c`, "a &lt; b &gt; c"},
		{`plain`, "plain"},
	}
	for _, tt := range tests {
		got := Escape(tt.in)
		if got != tt.want {
			t.Errorf("Escape(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTextAutoEscapes(t *testing.T) {
	h := Text("<script>alert('xss')</script>")
	got := h.String()
	if strings.Contains(got, "<script>") {
		t.Fatalf("Text should auto-escape, got: %s", got)
	}
	want := "&lt;script&gt;alert(&#39;xss&#39;)&lt;/script&gt;"
	if got != want {
		t.Errorf("Text(XSS) = %q, want %q", got, want)
	}
}

// --- Raw -------------------------------------------------------------------

func TestRawPassesThrough(t *testing.T) {
	in := "<em>bold</em>"
	got := Raw(in).String()
	if got != in {
		t.Errorf("Raw(%q) = %q, want %q", in, got, in)
	}
}

// --- Tag -------------------------------------------------------------------

func TestTagBasic(t *testing.T) {
	got := Tag("div", nil, Text("hello")).String()
	want := "<div>hello</div>"
	if got != want {
		t.Errorf("Tag(div) = %q, want %q", got, want)
	}
}

func TestTagWithAttrs(t *testing.T) {
	got := Tag("a", map[string]string{"href": "/", "class": "link"}, Text("home")).String()
	if !strings.Contains(got, `href="/"`) {
		t.Errorf("Tag(a) missing href attr: %s", got)
	}
	if !strings.Contains(got, `class="link"`) {
		t.Errorf("Tag(a) missing class attr: %s", got)
	}
	if !strings.HasPrefix(got, "<a ") || !strings.HasSuffix(got, "</a>") {
		t.Errorf("Tag(a) malformed: %s", got)
	}
}

func TestTagNoChildren(t *testing.T) {
	got := Tag("span", nil).String()
	want := "<span></span>"
	if got != want {
		t.Errorf("Tag(span, no children) = %q, want %q", got, want)
	}
}

// --- Nested tags -----------------------------------------------------------

func TestNestedTags(t *testing.T) {
	inner := Tag("em", nil, Text("world"))
	outer := Tag("p", nil, Text("hello "), inner)
	got := outer.String()
	want := "<p>hello <em>world</em></p>"
	if got != want {
		t.Errorf("nested tags = %q, want %q", got, want)
	}
}

// --- VoidTag ---------------------------------------------------------------

func TestVoidTag(t *testing.T) {
	got := VoidTag("img", map[string]string{"src": "pic.jpg", "alt": "photo"}).String()
	if !strings.HasPrefix(got, "<img ") {
		t.Errorf("VoidTag(img) = %q, should start with <img", got)
	}
	if strings.Contains(got, "</img>") {
		t.Errorf("VoidTag(img) should not have closing tag: %s", got)
	}
	if !strings.Contains(got, `src="pic.jpg"`) {
		t.Errorf("VoidTag(img) missing src: %s", got)
	}
	if !strings.Contains(got, `alt="photo"`) {
		t.Errorf("VoidTag(img) missing alt: %s", got)
	}
}

func TestVoidTagBr(t *testing.T) {
	got := VoidTag("br", nil).String()
	want := "<br>"
	if got != want {
		t.Errorf("VoidTag(br) = %q, want %q", got, want)
	}
}

// --- Join ------------------------------------------------------------------

func TestJoin(t *testing.T) {
	got := Join(Text("a"), Text("b"), Text("c")).String()
	want := "abc"
	if got != want {
		t.Errorf("Join = %q, want %q", got, want)
	}
}

// --- Attr ------------------------------------------------------------------

func TestAttr(t *testing.T) {
	got := Attr("href", "/search?q=a&b")
	want := `href="/search?q=a&amp;b"`
	if got != want {
		t.Errorf("Attr = %q, want %q", got, want)
	}
}

// --- If / When / Classes ---------------------------------------------------

func TestIf(t *testing.T) {
	if got := If(true, Text("yes")).String(); got != "yes" {
		t.Errorf("If(true) = %q, want %q", got, "yes")
	}
	if got := If(false, Text("yes")).String(); got != "" {
		t.Errorf("If(false) = %q, want empty", got)
	}
}

func TestWhenLazy(t *testing.T) {
	calls := 0
	fn := func() HTML {
		calls++
		return Text("yes")
	}
	When(false, fn)
	if calls != 0 {
		t.Errorf("When(false) called fn, got %d calls", calls)
	}
	When(true, fn)
	if calls != 1 {
		t.Errorf("When(true) calls = %d, want 1", calls)
	}
}

func TestClasses(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{[]string{"a", "b", "c"}, "a b c"},
		{[]string{"a", "", "c"}, "a c"},
		{[]string{"", "", ""}, ""},
		{[]string{"only"}, "only"},
		{nil, ""},
	}
	for _, tt := range tests {
		if got := Classes(tt.in...); got != tt.want {
			t.Errorf("Classes(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestClassIf(t *testing.T) {
	if got := ClassIf(true, "active"); got != "active" {
		t.Errorf("ClassIf(true, active) = %q, want active", got)
	}
	if got := ClassIf(false, "active"); got != "" {
		t.Errorf("ClassIf(false, active) = %q, want empty", got)
	}
}

func TestClassesWithClassIf(t *testing.T) {
	got := Classes("base", ClassIf(true, "active"), ClassIf(false, "error"))
	want := "base active"
	if got != want {
		t.Errorf("Classes+ClassIf = %q, want %q", got, want)
	}
}

// --- Component -------------------------------------------------------------

type GreetingData struct {
	Name string
}

func GreetingComponent(d GreetingData) HTML {
	return Tag("span", map[string]string{"class": "greeting"},
		Text("Hello, "+d.Name+"!"),
	)
}

func TestComponentRenders(t *testing.T) {
	// Register
	RegisterComponent("greeting", GreetingComponent)

	// Retrieve
	fn, ok := GetComponent("greeting")
	if !ok {
		t.Fatal("expected to find registered component 'greeting'")
	}
	greeting, ok := fn.(func(GreetingData) HTML)
	if !ok {
		t.Fatal("component has wrong type")
	}

	html := greeting(GreetingData{Name: "World"})
	got := html.String()
	want := `<span class="greeting">Hello, World!</span>`
	if got != want {
		t.Errorf("component output = %q, want %q", got, want)
	}
}

// --- Layout ----------------------------------------------------------------

func TestLayoutWraps(t *testing.T) {
	layout := func(title string, content HTML) HTML {
		return Join(
			Tag("head", nil, Tag("title", nil, Text(title))),
			Tag("body", nil, content),
		)
	}
	RegisterLayout("default", layout)

	pageContent := Tag("h1", nil, Text("Welcome"))
	result := RenderWithLayout("default", "Home", pageContent)

	got := result.String()
	if !strings.Contains(got, "<title>Home</title>") {
		t.Errorf("layout missing title: %s", got)
	}
	if !strings.Contains(got, "<h1>Welcome</h1>") {
		t.Errorf("layout missing content: %s", got)
	}
	if !strings.Contains(got, "<body>") || !strings.Contains(got, "</body>") {
		t.Errorf("layout missing body tag: %s", got)
	}
}

// --- Compose: layout + component ------------------------------------------

func TestComposeLayoutPlusComponent(t *testing.T) {
	nav := func() HTML {
		return Tag("nav", nil,
			Tag("a", map[string]string{"href": "/"}, Text("Home")),
		)
	}
	layout := func(title string, content HTML) HTML {
		return Join(
			Tag("head", nil, Tag("title", nil, Text(title))),
			Tag("body", nil, nav(), content),
		)
	}
	RegisterLayout("app", layout)

	greetingFn := func(d GreetingData) HTML {
		return Tag("h1", nil, Text("Hi, "+d.Name))
	}
	RegisterComponent("hi", greetingFn)

	fn, _ := GetComponent("hi")
	g := fn.(func(GreetingData) HTML)

	page := g(GreetingData{Name: "Alice"})
	full := RenderWithLayout("app", "Greet", page)

	got := full.String()
	if !strings.Contains(got, "<nav>") {
		t.Errorf("compose missing nav: %s", got)
	}
	if !strings.Contains(got, "<h1>Hi, Alice</h1>") {
		t.Errorf("compose missing greeting: %s", got)
	}
	if !strings.Contains(got, "<title>Greet</title>") {
		t.Errorf("compose missing title: %s", got)
	}
}

// --- RespondHTML -----------------------------------------------------------

func TestRespondHTML(t *testing.T) {
	rec := httptest.NewRecorder()
	RespondHTML(rec, Tag("h1", nil, Text("ok")))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
	body := rec.Body.String()
	want := "<h1>ok</h1>"
	if body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

// --- HTMLHandler -----------------------------------------------------------

func TestHTMLHandler(t *testing.T) {
	handler := HTMLHandler(func(r *http.Request) HTML {
		return Tag("p", nil, Text("handler"))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	want := "<p>handler</p>"
	if body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

// --- FuncMap ---------------------------------------------------------------

func TestFuncMapTruncate(t *testing.T) {
	fn := FuncMap["Truncate"].(func(string, int) string)
	got := fn("hello world", 5)
	want := "hello…"
	if got != want {
		t.Errorf("Truncate = %q, want %q", got, want)
	}
}

func TestRegisterFunc(t *testing.T) {
	RegisterFunc("double", func(s string) string { return s + s })
	fn := FuncMap["double"].(func(string) string)
	if fn("x") != "xx" {
		t.Error("custom func did not register correctly")
	}
}
