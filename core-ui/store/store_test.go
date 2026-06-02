package store

import (
	"context"
	"strings"
	"testing"
)

func TestSliceNameNamespaced(t *testing.T) {
	resetForTest()
	s := New("org")
	name := s.String("companyName", "Acme").Name()
	if name != "org.companyName" {
		t.Fatalf("name = %q, want org.companyName", name)
	}
}

func TestSliceNameNoNamespace(t *testing.T) {
	resetForTest()
	if got := New("").String("x", "").Name(); got != "x" {
		t.Fatalf("name = %q, want x", got)
	}
}

func TestBindEmitsAttrAndValue(t *testing.T) {
	resetForTest()
	name := New("org").String("companyName", "Acme Corp")
	html := string(name.Bind(context.Background(), "span", map[string]string{"class": "site-name"}))
	if !strings.Contains(html, `data-fui-signal="org.companyName"`) {
		t.Errorf("missing binding attr: %s", html)
	}
	if !strings.Contains(html, `class="site-name"`) {
		t.Errorf("missing passthrough attr: %s", html)
	}
	if !strings.Contains(html, ">Acme Corp<") {
		t.Errorf("initial value not stamped: %s", html)
	}
}

func TestBindStampsResolvedValue(t *testing.T) {
	resetForTest()
	name := New("org").String("companyName", "DefaultCo")
	ctx := WithValues(context.Background())
	name.Seed(ctx, "TenantCo")
	// Bind must stamp the SAME value the seed carries (request value),
	// or the DOM and _signals diverge on first paint.
	html := string(name.Bind(ctx, "span", nil))
	if !strings.Contains(html, ">TenantCo<") {
		t.Fatalf("Bind did not stamp the resolved request value: %s", html)
	}
}

func TestBindStampsIntValue(t *testing.T) {
	resetForTest()
	cnt := New("cart").Int("count", 7)
	html := string(cnt.Bind(context.Background(), "strong", nil))
	if !strings.Contains(html, `data-fui-signal="cart.count"`) || !strings.Contains(html, ">7<") {
		t.Errorf("int bind wrong: %s", html)
	}
}

func TestBindAttrMode(t *testing.T) {
	resetForTest()
	logo := New("org").String("companyName", "Acme")
	html := string(logo.BindAttr(context.Background(), "img", "alt", map[string]string{"src": "/logo.png"}))
	for _, want := range []string{
		`data-fui-signal="org.companyName"`,
		`data-fui-signal-mode="attr"`,
		`data-fui-signal-attr="alt"`,
		`alt="Acme"`,
		`src="/logo.png"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("attr-mode missing %q: %s", want, html)
		}
	}
}

func TestBindEscapesValue(t *testing.T) {
	resetForTest()
	s := New("x").String("v", `<script>alert(1)</script>`)
	html := string(s.Bind(context.Background(), "span", nil))
	if strings.Contains(html, "<script>alert") {
		t.Fatalf("text value not escaped — XSS: %s", html)
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("expected HTML-escaped value: %s", html)
	}
}

func TestDuplicateSameDefaultIsIdempotent(t *testing.T) {
	resetForTest()
	New("a").String("b", "1")
	// identical re-declaration must NOT panic (package re-init / shared decl)
	New("a").String("b", "1")
}

func TestDuplicateConflictingDefaultPanics(t *testing.T) {
	resetForTest()
	New("a").String("b", "1")
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on conflicting re-declaration")
		}
	}()
	New("a").String("b", "DIFFERENT")
}

func TestInvalidNamePanics(t *testing.T) {
	resetForTest()
	for _, bad := range []string{`a"b`, "a<b", "a b", "a\nb", "a]b"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("expected panic for invalid name %q", bad)
				}
			}()
			New("").String(bad, "")
		}()
	}
}

func TestScanReferencedFindsAllForms(t *testing.T) {
	resetForTest()
	html := `<span data-fui-signal="a">x</span>` +
		`<button data-fui-signal-set="b:1">s</button>` +
		`<button data-fui-signal-inc="c:2">+</button>` +
		`<button data-fui-signal-toggle="d">t</button>` +
		`<h1 data-fui-computed="e">e</h1>`
	got := ScanReferenced(html)
	want := map[string]bool{"a": true, "b": true, "c": true, "d": true, "e": true}
	if len(got) != len(want) {
		t.Fatalf("scan got %v, want keys %v", got, want)
	}
	for _, n := range got {
		if !want[n] {
			t.Errorf("unexpected name %q", n)
		}
	}
}

func TestScanIgnoresAttrInTextContent(t *testing.T) {
	resetForTest()
	// A literal mention inside text/code must not be treated as a binding.
	html := `<pre>data-fui-signal="ghost"</pre><span data-fui-signal="real">x</span>`
	got := ScanReferenced(html)
	for _, n := range got {
		if n == "ghost" {
			t.Fatalf("scan matched a non-attribute mention: %v", got)
		}
	}
}

func TestResolveSeedPrefersRequestValue(t *testing.T) {
	resetForTest()
	name := New("org").String("companyName", "DefaultCo")
	ctx := WithValues(context.Background())
	name.Seed(ctx, "TenantCo")

	seed := ResolveSeed(ctx, []string{"org.companyName"})
	if seed["org.companyName"] != "TenantCo" {
		t.Fatalf("seed = %v, want request value TenantCo", seed["org.companyName"])
	}
}

func TestResolveSeedFallsBackToDefault(t *testing.T) {
	resetForTest()
	New("org").String("companyName", "DefaultCo")
	seed := ResolveSeed(context.Background(), []string{"org.companyName"})
	if seed["org.companyName"] != "DefaultCo" {
		t.Fatalf("seed = %v, want default DefaultCo", seed["org.companyName"])
	}
}

func TestResolveSeedSkipsUnregistered(t *testing.T) {
	resetForTest()
	seed := ResolveSeed(context.Background(), []string{"never.declared"})
	if _, ok := seed["never.declared"]; ok {
		t.Fatalf("unregistered name should not seed: %v", seed)
	}
}

func TestGlobalNamesOnlyGlobalScope(t *testing.T) {
	resetForTest()
	New("a").String("page", "p")          // ScopePage (default)
	New("a").String("glob", "g").Global() // ScopeGlobal
	names := GlobalNames()
	if len(names) != 1 || names[0] != "a.glob" {
		t.Fatalf("GlobalNames = %v, want [a.glob]", names)
	}
}
