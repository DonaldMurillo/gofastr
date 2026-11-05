package inflect

import "testing"

func TestSingularIeNouns(t *testing.T) {
	for plural, singular := range map[string]string{
		"cookies": "cookie", "brownies": "brownie", "calories": "calorie",
		"zombies": "zombie", "rookies": "rookie", "pies": "pie",
		"fortune_cookies": "fortune_cookie", "home_movies": "home_movie", "bow_ties": "bow_tie", "UserTies": "UserTie", "Cookies": "Cookie",
		"series": "series", "species": "species", "Series": "Series",
		// -y stems whose tail spells an -ie noun stay -y stems.
		"parties": "party", "copies": "copy", "allies": "ally",
		"families": "family", "puppies": "puppy", "lies": "lie",
	} {
		if got := Singular(plural); got != singular {
			t.Errorf("Singular(%q) = %q, want %q", plural, got, singular)
		}
	}
}
