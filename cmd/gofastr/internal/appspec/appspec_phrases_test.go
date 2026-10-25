package appspec

import (
	"testing"
)

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
