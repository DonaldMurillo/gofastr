package appspec

import (
	"strings"
	"testing"
)

func TestPhraseCollectorDedupesAndNumbers(t *testing.T) {
	p := &AppSpecPhrases{Ns: "shop.screens.home", Prefix: "homeScreenText", ByText: map[string]string{}, Keys: map[string]bool{}, Vars: map[string]bool{}}
	if p.Used() || p.Block() != "" {
		t.Fatal("an empty collector must declare nothing")
	}
	first := p.Use("Sign in")
	if again := p.Use("Sign in"); again != first {
		t.Fatalf("the same English twice gave %q and %q, want one phrase", first, again)
	}
	second := p.Use("Sign in!")
	if first != "homeScreenTextSignIn" || second != "homeScreenTextSignIn2" {
		t.Fatalf("names = %q, %q; want homeScreenTextSignIn and homeScreenTextSignIn2", first, second)
	}
	block := p.Block()
	i, j := strings.Index(block, `"shop.screens.home.sign_in", "Sign in"`), strings.Index(block, `"shop.screens.home.sign_in_2", "Sign in!"`)
	if i < 0 || j < 0 || j < i {
		t.Fatalf("block does not declare both phrases in first-use order:\n%s", block)
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
