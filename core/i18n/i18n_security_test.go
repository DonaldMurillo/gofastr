package i18n

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAcceptLanguageBounded asserts that parsing of the attacker-controlled
// Accept-Language header is bounded so a single request cannot force large
// allocation + O(n log n) sort work over hundreds of thousands of segments.
func TestAcceptLanguageBounded(t *testing.T) {
	const cap = 32

	build := func(seg string, n int) string {
		b := make([]byte, 0, len(seg)*n)
		for i := range n {
			if i > 0 {
				b = append(b, ',')
			}
			b = append(b, seg...)
		}
		return string(b)
	}

	cases := []struct {
		name   string
		header string
	}{
		{"happy path", "fr-CA,fr;q=0.8,en;q=0.5"},
		{"comma flood bare tags", build("a", 200000)},
		{"comma flood with q", build("a;q=1.0", 200000)},
		{"empty segments flood", build("", 200000)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAcceptLanguage(tc.header)
			if len(got) > cap {
				t.Fatalf("parseAcceptLanguage returned %d entries; want <= %d (unbounded parse is a DoS amplifier)", len(got), cap)
			}
		})
	}

	// The happy path must still negotiate correctly (preferred tag first).
	if got := parseAcceptLanguage("fr-CA,fr;q=0.8,en;q=0.5"); len(got) == 0 || got[0] != "fr-ca" {
		t.Fatalf("happy-path ordering broken: got %v", got)
	}
}

// TestAcceptLangWorkIsBounded pins the WORK, not only the result size.
// TestAcceptLanguageBounded above caps len(out) at 32, but the cap was
// consulted only after strings.Split had already materialised the whole
// comma-separated slice: a ~600KB header still allocated ~3.2MB and
// ~500k strings per request (measured 9360x the time and 12521x the
// bytes of a normal header) before a single entry was rejected. The
// sort and the out slice were bounded; the split was not.
func TestAcceptLangWorkIsBounded(t *testing.T) {
	// Measure BYTES, not object count: strings.Split allocated a single
	// 200k-element backing array, so an allocs/op assertion would have
	// stayed green while 3.2MB churned per request.
	bytesFor := func(header string) uint64 {
		res := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				parseAcceptLanguage(header)
			}
		})
		return uint64(res.AllocedBytesPerOp())
	}

	normal := bytesFor("en-US,en;q=0.9,fr;q=0.8")
	// A bounded parser allocates the same handful of bytes regardless of
	// how many separators the attacker packs in.
	budget := normal*8 + 4096

	if got := bytesFor(strings.Repeat("en,", 200000)); got > budget {
		t.Errorf("SECURITY: [i18n] a comma-flooded Accept-Language allocated %d B/op "+
			"against %d B/op for a normal header (budget %d). Attack: every "+
			"unauthenticated request amplifies a header into megabytes of garbage.",
			got, normal, budget)
	}
	if got := bytesFor("en;" + strings.Repeat("q=0.1;", 200000)); got > budget {
		t.Errorf("SECURITY: [i18n] a semicolon-flooded Accept-Language allocated %d B/op "+
			"(budget %d). Attack: the inner parameter split is uncapped.", got, budget)
	}

	// Correctness survives the bounding.
	if got := parseAcceptLanguage("fr-CA,fr;q=0.8,en;q=0.5"); len(got) == 0 || got[0] != "fr-ca" {
		t.Fatalf("[i18n] happy-path ordering broken: got %v", got)
	}
}

// TestNegotiatedTagIsBounded asserts the property sanitizeTag
// already states for cookies, "resolver values are attacker-controlled,
// so they are length- and character-bounded before any matching," holds
// at EVERY source Negotiate accepts, not only the resolver.
//
// X-Locale is equally attacker-controlled and wins OUTRIGHT, yet only
// got lower-case+trim; and with a nil Translator the entire raw
// Accept-Language header became the tag. Both flow into tagFallbacks →
// Catalog.Get, and Catalog is a documented third-party extension point
// whose own wiring example is LoadJSONCatalog(os.DirFS("locales"), ".").
// A host backing that interface with a lazily-loading FS or DB catalog
// received "../../etc/passwd" as a locale. The in-tree MapCatalog is a
// plain map lookup, which is why nothing caught it.
func TestNegotiatedTagIsBounded(t *testing.T) {
	tr := NewTranslator(NewMapCatalog(), "en")
	hostile := []string{
		"../../etc/passwd",
		"..%2f..%2fsecrets",
		strings.Repeat("a", 400),
		"en\nX-Injected: 1",
		"en/../../x",
	}

	for _, bad := range hostile {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-Locale", bad)
		if got := Negotiate(tr, r).Tag; got != "" && sanitizeTag(got) != got {
			t.Errorf("SECURITY: [i18n] X-Locale %q negotiated to unbounded tag %q. "+
				"Attack: the tag reaches a host Catalog.Get as a file/DB lookup key.", bad, got)
		}

		// nil Translator takes the raw-header branch.
		r2 := httptest.NewRequest(http.MethodGet, "/", nil)
		r2.Header.Set("Accept-Language", bad)
		if got := Negotiate(nil, r2).Tag; got != "" && sanitizeTag(got) != got {
			t.Errorf("SECURITY: [i18n] nil-translator Accept-Language %q negotiated to "+
				"unbounded tag %q. Attack: same Catalog.Get sink.", bad, got)
		}
	}

	// The documented "X-Locale wins outright" escape hatch survives for
	// any tag that IS a plausible BCP 47 value, catalog hit or not.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Locale", "ja")
	if got := Negotiate(tr, r).Tag; got != "ja" {
		t.Errorf("[i18n] X-Locale no longer wins outright: got %q want ja", got)
	}
}

// --- interpolation at the params boundary ----------------------------------
//
// Property: a parameter VALUE is substituted once and is never re-parsed
// as template syntax. Message templates are developer-supplied (trusted);
// params are runtime values — callers interpolate user-display names,
// emails, and other attacker-influenced strings — so a value containing
// {{...}} must land in the output verbatim, not gain template semantics
// (double expansion). HTML escaping is deliberately out of scope here:
// i18n produces plain strings; the render layer owns markup safety.
func TestInterpolatedValueNotReexpanded(t *testing.T) {
	cat := NewMapCatalog()
	cat.Set("en", "welcome", Message{Text: "Hi {{name}}!"})
	cat.Set("en", "items", Message{Plural: map[string]string{
		"one":   "{{count}} item for {{name}}",
		"other": "{{count}} items for {{name}}",
	}})
	tr := NewTranslator(cat, "en")
	ctx := context.Background()

	cases := []struct {
		key    string
		params map[string]any
		want   string
	}{
		{"welcome", map[string]any{"name": "{{admin}}"}, "Hi {{admin}}!"},
		{"welcome", map[string]any{"name": "{{name}}"}, "Hi {{name}}!"},
		{"items", map[string]any{"count": 2, "name": "{{count}}"}, "2 items for {{count}}"},
	}
	for _, c := range cases {
		if got := tr.T(ctx, c.key, c.params); got != c.want {
			t.Errorf("T(%q, %v) = %q, want %q — a param value was re-expanded as a template", c.key, c.params, got, c.want)
		}
	}

	// A value that LOOKS like a placeholder for an unknown name stays
	// visible (documented "unknown placeholders are left as-is"), and a
	// value cannot reference a param the message never declared.
	if got := tr.T(ctx, "welcome", map[string]any{"name": "{{secret}}"}); !strings.Contains(got, "{{secret}}") || strings.Contains(got, "(redacted)") {
		t.Errorf("value placeholder mangled: %q", got)
	}
}

// TestPluralCountHostileShapesSafe pins that plural selection only honors
// genuinely numeric count params: garbage-typed counts (bool, map,
// non-numeric string, integer-overflow string) fall back to the "other"
// variant instead of panicking or inventing a category.
func TestPluralCountHostileShapesSafe(t *testing.T) {
	cat := NewMapCatalog()
	cat.Set("en", "items", Message{Plural: map[string]string{
		"one":   "ONE",
		"other": "OTHER",
	}})
	tr := NewTranslator(cat, "en")
	ctx := context.Background()

	hostile := []map[string]any{
		{"count": "not-a-number"},
		{"count": true},
		{"count": map[string]any{}},
		{"count": "99999999999999999999999"},
		{"count": nil},
	}
	for _, p := range hostile {
		if got := tr.T(ctx, "items", p); got != "OTHER" {
			t.Errorf("T(items, %v) = %q, want the %q fallback for a non-numeric count", p, got, "OTHER")
		}
	}
	// The happy paths keep working on both spellings.
	if got := tr.T(ctx, "items", map[string]any{"count": 1}); got != "ONE" {
		t.Errorf("numeric count 1 = %q, want ONE", got)
	}
	if got := tr.T(ctx, "items", map[string]any{"n": "3"}); got != "OTHER" {
		t.Errorf(`string count "3" = %q, want OTHER`, got)
	}
}

// TestNilSurfacesReturnBareKey pins the documented fallback at every nil
// seam: a nil Translator, a Translator with a nil catalog, and a catalog
// missing the locale all return the bare key — never a panic, which would
// turn a boot-order race into a request-time crash.
func TestNilSurfacesReturnBareKey(t *testing.T) {
	ctx := context.Background()
	var nilTr *Translator
	if got := nilTr.T(ctx, "greeting"); got != "greeting" {
		t.Errorf("nil Translator T = %q, want bare key", got)
	}
	if tr := NewTranslator(nil, "en"); tr.T(ctx, "greeting") != "greeting" {
		t.Errorf("nil catalog T = %q, want bare key", tr.T(ctx, "greeting"))
	}
	cat := NewMapCatalog()
	cat.Set("en", "greeting", Message{Text: "hello"})
	if tr := NewTranslator(cat, ""); tr.T(context.WithValue(ctx, ctxKey{}, Locale{Tag: "fr"}), "greeting") != "greeting" {
		t.Errorf("missing-locale lookup should fall through to the bare key with no fallback configured")
	}
}
