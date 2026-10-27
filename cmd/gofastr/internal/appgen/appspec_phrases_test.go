package appgen

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

func TestScreenWordsArePhrases(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Shop", Module: "example.com/shop"},
		Screens: []appspec.AppSpecScreen{{
			Name: "home", Route: "/", Title: "Welcome",
			Body: []appspec.AppSpecBlock{
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
	p := appSpecCrudPhrases("Shop", "orders", "orders_crud")
	s := appSpecScreenPhrases("Shop", "orders", "orders")
	c := appSpecChromePhrases("Shop")
	got := []string{p.Use("Orders"), s.Use("Orders"), c.Use("Orders")}
	if got[0] == got[1] || got[1] == got[2] || got[0] == got[2] {
		t.Fatalf("variables collide across files: %v", got)
	}
	keys := []string{p.Decls[0].Key, s.Decls[0].Key, c.Decls[0].Key}
	want := []string{"shop.screens.orders_crud.orders", "shop.screens.orders.orders", "shop.chrome.orders"}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("key %d = %q, want %q", i, keys[i], want[i])
		}
	}
}

// Screens whose names fold to one key scope ("PostsList" and "postslist",
// distinct types and files) get their own: one key with
// two English texts makes i18n.Define panic when the app starts. A
// screen whose scope nothing else claims keeps its key.
func TestFoldedScreenNamesOwnKeys(t *testing.T) {
	screen := func(name, route, title string) appspec.AppSpecScreen {
		return appspec.AppSpecScreen{Name: name, Route: route, Title: title,
			Body: []appspec.AppSpecBlock{{Type: "paragraph", Text: title}}}
	}
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Shop", Module: "example.com/shop"},
		Screens: []appspec.AppSpecScreen{
			screen("PostsList", "/a", "Posts"),
			screen("postslist", "/b", "Posts!"),
			screen("home", "/", "Welcome"),
		},
	}
	if err := appspec.ValidateAppSpec(bp); err != nil {
		t.Fatalf("the spec must be one generate accepts: %v", err)
	}
	english := map[string]string{}
	define := regexp.MustCompile(`i18n\.Define\(("(?:[^"\\]|\\.)*"), ("(?:[^"\\]|\\.)*")\)`)
	for _, f := range mustRenderAppSpecFiles(t, bp) {
		for _, m := range define.FindAllStringSubmatch(f.Content, -1) {
			key, _ := strconv.Unquote(m[1])
			text, _ := strconv.Unquote(m[2])
			if prev, dup := english[key]; dup && prev != text {
				t.Errorf("%s: key %s says %q and %q", f.Name, key, prev, text)
			}
			english[key] = text
		}
	}
	if english["shop.screens.home.welcome"] != "Welcome" {
		t.Errorf("the home screen's key moved: %v", english)
	}
}

func TestPhraseSlugCollisionNumbered(t *testing.T) {
	p := appSpecScreenPhrases("Shop", "home", "home")
	a := p.Use("Sign in")
	b := p.Use("Sign in!")
	if a == b {
		t.Fatalf("two English strings share %s", a)
	}
	if p.Decls[1].Key != "shop.screens.home.sign_in_2" || b != "homeScreenTextSignIn2" {
		t.Errorf("second phrase = %s %s", p.Decls[1].Key, b)
	}
}
