package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// inlinePhrases rewrites every phrase read in generated Go source back
// to the English it says, the way pack reads it (packPhraseEnglish),
// and leaves every other byte as the generator wrote it. Tests that pin
// what a screen says read the English through it; the phrase tests
// below pin the phrase spelling itself. Source that does not parse is
// returned unchanged.
func inlinePhrases(src string) string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		return src
	}
	english := map[string]string{}
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			if len(vs.Values) != 1 {
				continue
			}
			if call, ok := vs.Values[0].(*ast.CallExpr); ok && packIsSel(call.Fun, "i18n", "Define") && len(call.Args) == 2 {
				english[vs.Names[0].Name], _ = astStringLit(call.Args[1])
			}
		}
	}
	type span struct {
		start, end int
		lit        string
	}
	var spans []span
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if en, ok := packPhraseEnglish(call, english); ok {
			spans = append(spans, span{fset.Position(call.Pos()).Offset, fset.Position(call.End()).Offset, strconv.Quote(en)})
			return false
		}
		return true
	})
	sort.Slice(spans, func(i, j int) bool { return spans[i].start > spans[j].start })
	for _, s := range spans {
		src = src[:s.start] + s.lit + src[s.end:]
	}
	return src
}

func TestScreenWordsArePhrases(t *testing.T) {
	bp := AppSpec{
		App: AppSpecApp{Name: "Shop", Module: "example.com/shop"},
		Screens: []AppSpecScreen{{
			Name: "home", Route: "/", Title: "Welcome",
			Body: []AppSpecBlock{
				{Kind: "page_header", Props: map[string]any{"title": "Welcome", "subtitle": "Fresh bread daily."}},
				{Type: "paragraph", Text: "Fresh bread daily."},
			},
		}},
	}
	src := filesByName(mustRenderAppSpecFiles(t, bp))["screen_home.go"]
	for _, want := range []string{
		`"github.com/DonaldMurillo/gofastr/core/i18n"`,
		`homeScreenTextWelcome = i18n.Define("shop.screens.home.welcome", "Welcome")`,
		`homeScreenTextFreshBreadDaily = i18n.Define("shop.screens.home.fresh_bread_daily", "Fresh bread daily.")`,
		`ScreenTitle(ctx context.Context) string { return homeScreenTextWelcome.Text(ctx) }`,
		`ui.PageHeaderConfig{Title: homeScreenTextWelcome.Text(ctx), Subtitle: homeScreenTextFreshBreadDaily.Text(ctx)`,
		`render.Text(homeScreenTextFreshBreadDaily.Text(ctx))`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("screen_home.go is missing %s\n%s", want, src)
		}
	}
	// The same English said three times is one phrase.
	if n := strings.Count(src, `"Fresh bread daily."`); n != 1 {
		t.Errorf("English declared %d times, want 1:\n%s", n, src)
	}
}

func TestCrudPhrasesOwnNamespace(t *testing.T) {
	p := appSpecCrudPhrases("Shop", "orders")
	s := appSpecScreenPhrases("Shop", "orders")
	c := appSpecChromePhrases("Shop")
	got := []string{p.use("Orders"), s.use("Orders"), c.use("Orders")}
	if got[0] == got[1] || got[1] == got[2] || got[0] == got[2] {
		t.Fatalf("variables collide across files: %v", got)
	}
	keys := []string{p.decls[0].key, s.decls[0].key, c.decls[0].key}
	want := []string{"shop.screens.orders_crud.orders", "shop.screens.orders.orders", "shop.chrome.orders"}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("key %d = %q, want %q", i, keys[i], want[i])
		}
	}
}

func TestPhraseSlugRules(t *testing.T) {
	for in, want := range map[string]string{
		"Sign in":                         "sign_in",
		"Don't miss it":                   "dont_miss_it",
		"One two three four five six":     "one_two_three_four",
		"30-day trial":                    "n30_day_trial",
		"日本語":                             "text",
		"  — Already a member? Sign in —": "already_a_member_sign",
	} {
		if got := appSpecPhraseSlug(in); got != want {
			t.Errorf("appSpecPhraseSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPhraseSlugCollisionNumbered(t *testing.T) {
	p := appSpecScreenPhrases("Shop", "home")
	a := p.use("Sign in")
	b := p.use("Sign in!")
	if a == b {
		t.Fatalf("two English strings share %s", a)
	}
	if p.decls[1].key != "shop.screens.home.sign_in_2" || b != "homeScreenTextSignIn2" {
		t.Errorf("second phrase = %s %s", p.decls[1].key, b)
	}
}

func TestPackInlinesScreenPhrases(t *testing.T) {
	bp := AppSpec{
		App: AppSpecApp{Name: "Shop", Module: "example.com/shop"},
		Screens: []AppSpecScreen{{
			Name: "home", Route: "/", Title: "Welcome",
			Body: []AppSpecBlock{{Kind: "page_header", Props: map[string]any{"title": "Hello"}}},
		}},
	}
	src := filesByName(mustRenderAppSpecFiles(t, bp))["screen_home.go"]
	file, err := parser.ParseFile(token.NewFileSet(), "screen_home.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	packResolvePhrases(file)
	var lits []string
	ast.Inspect(file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Text" {
			if id, ok := sel.X.(*ast.Ident); ok && strings.HasPrefix(id.Name, "homeScreenText") {
				t.Errorf("phrase read %s.Text left unresolved", id.Name)
			}
		}
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			lits = append(lits, lit.Value)
		}
		return true
	})
	joined := strings.Join(lits, " ")
	for _, want := range []string{`"Welcome"`, `"Hello"`} {
		if strings.Count(joined, want) < 2 { // the Define and the resolved read
			t.Errorf("%s not read back as a literal: %s", want, joined)
		}
	}
}
