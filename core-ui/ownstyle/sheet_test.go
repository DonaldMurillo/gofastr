package ownstyle

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

const cardCSS = "/* One issue. */\n:scope { display: grid; gap: var(--spacing-xs); }\n"

// ownIsolate swaps the process-global style registry for a fresh one so
// Must tests can register freely. Sequential tests only, per
// registry.IsolateForTest's contract.
func ownIsolate(t *testing.T) {
	t.Helper()
	registry.IsolateForTest(t)
}

func TestMustRegistersScopedSheet(t *testing.T) {
	ownIsolate(t)
	s := Must("issuecard", KindScoped, cardCSS)
	if s.Name() != "issuecard" || s.Kind() != KindScoped {
		t.Fatalf("handle: %s %s", s.Name(), s.Kind())
	}
	entry, ok := registry.Lookup("issuecard")
	if !ok {
		t.Fatal("not registered")
	}
	if entry.Load != registry.LoadAuto {
		t.Errorf("scoped sheet load mode: %d", entry.Load)
	}
	// The served CSS is the compiled form, per theme.
	tok := style.ThemeToTokens(style.DefaultTheme())
	want, err := Compile(mustParse(t, cardCSS), "issuecard", KindScoped, tok)
	if err != nil {
		t.Fatal(err)
	}
	if got := entry.CSSFor(style.DefaultTheme()); got != want {
		t.Errorf("served CSS:\n got %s\nwant %s", got, want)
	}
	// And it follows the theme: md moved to 800px moves the query.
	themeCSS := ":scope { color: red; }\n@media (--above-md) { :scope { color: blue; } }\n"
	s2 := Must("themed", KindScoped, themeCSS)
	theme := style.DefaultTheme()
	theme.Breakpoints.MD.Value = 800
	if got := s2.entry.CSSFor(theme); !strings.Contains(got, "(min-width: 800px)") {
		t.Errorf("theme not followed: %s", got)
	}
}

func TestMustRegistersAppSheetLoadAlways(t *testing.T) {
	ownIsolate(t)
	s := Must("app", KindApp, ".figure{letter-spacing:-0.01em}")
	entry, _ := registry.Lookup("app")
	if entry.Load != registry.LoadAlways {
		t.Errorf("app sheet load mode: %d", entry.Load)
	}
	if !strings.Contains(entry.CSSFor(style.DefaultTheme()), "@scope (:root) to ([data-fui-internal])") {
		t.Errorf("app wrap missing: %s", entry.CSSFor(style.DefaultTheme()))
	}
	_ = s
}

func mustParse(t *testing.T, css string) *Stylesheet {
	t.Helper()
	sheet, diags := Parse(css)
	if len(diags) != 0 {
		t.Fatalf("parse diags: %v", diags)
	}
	return sheet
}

func TestMustPanics(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
		want string
	}{
		{"duplicate", func() {
			Must("card", KindScoped, cardCSS)
			Must("card", KindScoped, ":scope{color:red}")
		}, `an owned style named "card" is already registered`},
		{"upper", func() { Must("IssueCard", KindScoped, cardCSS) }, "not a valid style name"},
		{"kit prefix", func() { Must("ui-card", KindScoped, cardCSS) }, "not a valid style name"},
		{"slash", func() { Must("a/b", KindScoped, cardCSS) }, "not a valid style name"},
		{"empty", func() { Must("", KindScoped, cardCSS) }, "not a valid style name"},
		{"app name on scoped", func() { Must("app", KindScoped, cardCSS) }, `"app" is reserved`},
		{"app kind wrong name", func() { Must("tracker", KindApp, cardCSS) }, `the app owner's name is "app"`},
		{"parse error", func() { Must("card", KindScoped, ".a { color: red") }, "hand-edited or stale"},
		{"unknown custom media", func() {
			Must("card", KindScoped, "@media (--above-3xl) { :scope { color: red; } }")
		}, "unknown custom media"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ownIsolate(t)
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("no panic; want one mentioning %q", tc.want)
				}
				if msg, ok := r.(string); !ok || !strings.Contains(msg, tc.want) {
					t.Fatalf("panic %v does not mention %q", r, tc.want)
				}
			}()
			tc.fn()
		})
	}
}

func TestScopeStampsOnlyTheScopeMarker(t *testing.T) {
	ownIsolate(t)
	s := Must("issuecard", KindScoped, cardCSS)
	root := render.HTML(`<a class="card" href="/x">body</a>`)
	got := string(s.Scope(root))
	want := `<a class="card" href="/x" data-fui-scope="issuecard">body</a>`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if strings.Contains(got, "data-fui-comp") {
		t.Fatalf("Scope stamped a loader marker; owned sheets load by data-fui-scope:\n%s", got)
	}
	// Same owner again is a no-op.
	if again := string(s.Scope(render.HTML(got))); again != got {
		t.Fatalf("not idempotent:\n%s", again)
	}
}

func TestScopeOnKitRootKeepsCompAndGainsScope(t *testing.T) {
	// Style.Scope(ui.Card(...)): the kit root already carries
	// data-fui-comp="ui-card" (the kit sheet's loader marker). Scope
	// must keep it and add only data-fui-scope — a second comp marker
	// would collide with the kit's own loading.
	ownIsolate(t)
	s := Must("issuecard", KindScoped, cardCSS)
	root := render.HTML(`<div class="fui-card" data-fui-comp="ui-card"><div class="fui-card__body"></div></div>`)
	got := string(s.Scope(root))
	want := `<div class="fui-card" data-fui-comp="ui-card" data-fui-scope="issuecard"><div class="fui-card__body"></div></div>`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestScopePanicsOnDifferentOwner(t *testing.T) {
	ownIsolate(t)
	inner := Must("issuecard", KindScoped, cardCSS)
	root := inner.Scope(render.HTML(`<div class="card"></div>`))
	outer := Must("inbox", KindScoped, cardCSS)
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("no panic for a second owner on one element")
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, "issuecard") || !strings.Contains(msg, "Nest") {
			t.Fatalf("panic %v does not name the owner or the fix", r)
		}
	}()
	outer.Scope(root)
}

func TestScopePanicsOnAppAndMalformed(t *testing.T) {
	ownIsolate(t)
	app := Must("app", KindApp, ".a{color:red}")
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("Scope on app sheet did not panic")
			}
		}()
		app.Scope(render.HTML(`<div></div>`))
	}()
	s := Must("issuecard", KindScoped, cardCSS)
	for _, bad := range []string{`hello`, `<div`, `</div>`, `<!-- c --><div`} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("Scope(%q) did not panic", bad)
				}
			}()
			s.Scope(render.HTML(bad))
		}()
	}
}

func TestMustPanicsOnDeclAfterNestedRule(t *testing.T) {
	ownIsolate(t)
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("no panic for a declaration after a nested rule")
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, "declaration after a nested rule") {
			t.Fatalf("panic %v does not name the problem", r)
		}
	}()
	Must("card", KindScoped, ".a { & { color: red; } color: blue; }")
}
