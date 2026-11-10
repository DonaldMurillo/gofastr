package inflect

import "testing"

// Regular plurals whose suffix rule cut the wrong letters: an admin
// heading read "New Aliase", "New Quizze", "New Cach". Each comes back as
// its singular, in any position a word can end an entity name.
func TestSingularWordsTheSuffixRulesMangled(t *testing.T) {
	for plural, singular := range map[string]string{
		"aliases": "alias", "canvases": "canvas", "biases": "bias", "atlases": "atlas",
		"gases": "gas", "lenses": "lens", "quizzes": "quiz", "caches": "cache",
		"niches": "niche", "heroes": "hero", "potatoes": "potato", "tomatoes": "tomato",
		"echoes": "echo", "menus": "menu", "news": "news",
		"Aliases": "Alias", "email_aliases": "email_alias", "Email Aliases": "Email Alias",
		"userQuizzes": "userQuiz", "page-caches": "page-cache", "MENUS": "MENU",
		// The neighbours the rules already had right stay right.
		"databases": "database", "cases": "case", "phases": "phase", "licenses": "license",
		"expenses": "expense", "matches": "match", "coaches": "coach", "shoes": "shoe",
		"statuses": "status", "bonus": "bonus", "views": "view",
	} {
		if got := Singular(plural); got != singular {
			t.Errorf("Singular(%q) = %q, want %q", plural, got, singular)
		}
	}
}
