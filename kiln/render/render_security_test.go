package render

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Property: no agent-authored world-IR string reaches a raw sink.
//
// Kiln's build mode mutates the IR over HTTP, so every world.* field below
// is attacker-authored. The sinks that matter in this package are the seed
// INSERT statement (SQL) and the theme map (the app stylesheet).

// TestSeedIdentRejectsInjection pins that a seed's entity name and its row
// keys are validated as SQL identifiers rather than merely quoted.
//
// The private quoteIdent this replaced escaped `"` and then wrote each rune
// with `append(out, byte(r))`, truncating to the low byte AFTER the escape
// test. Any rune whose low byte is 0x22 — U+2022 '•', U+0122, U+0222, … —
// became a literal quote the escape never saw, closing the identifier:
//
//	quoteIdent("users•) VALUES(1);--")  ->  "users") VALUES(1);--"
func TestSeedIdentRejectsInjection(t *testing.T) {
	db := openSeedDB(t)
	for name, seed := range map[string]*world.Seed{
		"truncating rune in table": {
			Entity: "users\u2022) VALUES('x'); DROP TABLE users; --",
			Rows:   []map[string]any{{"name": "a"}},
		},
		"truncating rune in column": {
			Entity: "users",
			Rows:   []map[string]any{{"name\u2022) VALUES('x'); --": "a"}},
		},
		"bare quote in table": {
			Entity: `users" ; DROP TABLE users; --`,
			Rows:   []map[string]any{{"name": "a"}},
		},
		"space and semicolon": {
			Entity: "users; DROP TABLE users",
			Rows:   []map[string]any{{"name": "a"}},
		},
		"empty column name": {
			Entity: "users",
			Rows:   []map[string]any{{"": "a"}},
		},
	} {
		err := ApplySeeds(db, &world.World{Seeds: []*world.Seed{seed}})
		if err == nil {
			t.Errorf("%s: seed accepted, want rejection", name)
		}
	}
	// The table must survive every attempt above.
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM users`).Scan(&n); err != nil {
		t.Fatalf("users table did not survive: %v", err)
	}
}

// TestSeedHappyPathStillInserts guards against an over-strict validator.
func TestSeedHappyPathStillInserts(t *testing.T) {
	db := openSeedDB(t)
	seed := &world.Seed{Entity: "users", Rows: []map[string]any{{"name": "ada"}}}
	if err := ApplySeeds(db, &world.World{Seeds: []*world.Seed{seed}}); err != nil {
		t.Fatalf("valid seed rejected: %v", err)
	}
	var got string
	if err := db.QueryRow(`SELECT name FROM users`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "ada" {
		t.Errorf("name = %q, want ada", got)
	}
}

func openSeedDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE users (name TEXT)`); err != nil {
		t.Fatal(err)
	}
	return db
}

// TestThemeRejectsCSSInjection pins that theme tokens from the IR cannot
// escape their declaration. core-ui/style emits `--color-<key>: <value>;`
// with no escaping of either side, so an unvalidated value closes the rule
// block and appends arbitrary CSS to the app stylesheet — a bypass of the
// node renderer's deliberate strip of `class` and inline styles.
func TestThemeRejectsCSSInjection(t *testing.T) {
	cfg := world.AppConfig{
		Theme: map[string]string{
			"primary":    "red; } body { background: url(//evil.test/x) } .z {",
			"background": "#fff",
			"text":       "expression(alert(1))",
			"font_body":  "Inter, sans-serif",
			"surface":    "url(//evil.test/leak)",
		},
		ThemeDark: map[string]string{
			"primary":                         "#000",
			"x: y; } :root { --color-primary": "#f00",
			"surface":                         "}\n@import url(//evil.test/x);\n:root{",
		},
	}
	th := worldTheme(cfg)
	css := th.CSSCustomProperties()
	for _, bad := range []string{"evil.test", "@import", "expression(", "body {"} {
		if strings.Contains(css, bad) {
			t.Errorf("theme CSS contains %q:\n%s", bad, css)
		}
	}
	// Well-formed values must still land.
	if !strings.Contains(css, "#fff") {
		t.Error("valid color dropped")
	}
	if !strings.Contains(css, "Inter") {
		t.Error("valid font dropped")
	}
	if th.DarkColors["primary"] != "#000" {
		t.Errorf("valid dark color dropped: %q", th.DarkColors["primary"])
	}
}

// TestUnsafeHrefDegradesNotPanics pins that a hostile href in the typed
// node path degrades like the leaf path does, instead of panicking SSR.
// ui.LinkButton panics on an unsafe scheme (a correct contract for a Go
// author passing a literal); kiln feeds it agent IR, so the check has to
// happen before the component sees it.
func TestUnsafeHrefDegradesNotPanics(t *testing.T) {
	for _, href := range []string{
		"javascript:alert(1)",
		"JaVaScRiPt:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"vbscript:msgbox(1)",
		"\tjavascript:alert(1)",
	} {
		for _, kind := range []string{"link_button", "card"} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("%s href=%q panicked: %v", kind, href, r)
					}
				}()
				out := string(RenderNode(world.Node{
					Kind:  kind,
					Props: map[string]any{"label": "go", "heading": "go", "href": href},
				}))
				if strings.Contains(strings.ToLower(out), "javascript:") ||
					strings.Contains(strings.ToLower(out), "vbscript:") ||
					strings.Contains(strings.ToLower(out), "data:text/html") {
					t.Errorf("%s emitted the unsafe href: %s", kind, out)
				}
			}()
		}
	}
}

// TestSafeHrefStillRenders guards against dropping legitimate links.
func TestSafeHrefStillRenders(t *testing.T) {
	out := string(RenderNode(world.Node{
		Kind:  "link_button",
		Props: map[string]any{"label": "go", "href": "/dashboard"},
	}))
	if !strings.Contains(out, "/dashboard") {
		t.Errorf("safe href dropped: %s", out)
	}
}

// TestKilnToolNameRejectsTraversal pins that an agent-authored
// data-kiln-tool value is a bare tool identifier and nothing else. The
// runtime concatenates it into a URL without encodeURIComponent, so a value
// with dot-segments, a query, or a fragment redirects the click away from
// /kiln/tool/ to an arbitrary same-origin route carrying the operator's
// cookies and CSRF token.
//
// The attribute stays supported — kiln/integration's
// TestBrowser_ButtonToolCallFires pins that a button naming a real tool
// still fires it, and that contract is deliberately kept.
func TestKilnToolNameRejectsTraversal(t *testing.T) {
	for _, name := range []string{
		"../../api/posts",
		"chat?x=1",
		"chat#frag",
		"a/b",
		"..%2fadmin",
	} {
		out := string(RenderNode(world.Node{
			Kind: "button",
			Props: map[string]any{
				"label":          "go",
				"data-kiln-tool": name,
				"data-kiln-args": `{"role":"user"}`,
			},
		}))
		if strings.Contains(out, "data-kiln-tool") {
			t.Errorf("tool name %q survived: %s", name, out)
		}
	}
}

// TestKilnToolNameKeepsRealTools guards the feature the rejection above
// must not break.
func TestKilnToolNameKeepsRealTools(t *testing.T) {
	for _, name := range []string{"chat", "add_page", "update_page_element"} {
		out := string(RenderNode(world.Node{
			Kind:  "button",
			Props: map[string]any{"label": "go", "data-kiln-tool": name},
		}))
		if !strings.Contains(out, `data-kiln-tool="`+name+`"`) {
			t.Errorf("real tool %q dropped: %s", name, out)
		}
	}
}
