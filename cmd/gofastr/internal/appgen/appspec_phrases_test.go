package appgen

import (
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
	p := appSpecCrudPhrases("Shop", "orders")
	s := appSpecScreenPhrases("Shop", "orders")
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

func TestPhraseSlugCollisionNumbered(t *testing.T) {
	p := appSpecScreenPhrases("Shop", "home")
	a := p.Use("Sign in")
	b := p.Use("Sign in!")
	if a == b {
		t.Fatalf("two English strings share %s", a)
	}
	if p.Decls[1].Key != "shop.screens.home.sign_in_2" || b != "homeScreenTextSignIn2" {
		t.Errorf("second phrase = %s %s", p.Decls[1].Key, b)
	}
}
