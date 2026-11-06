package inflect

import "testing"

func TestSingularRegularPlurals(t *testing.T) {
	for plural, singular := range map[string]string{
		"houses": "house", "causes": "cause", "blouses": "blouse", "pauses": "pause",
		"movies": "movie", "ties": "tie", "Movies": "Movie", "Ties": "Tie",
		"parties": "party", "cities": "city", "properties": "property", "statuses": "status",
	} {
		if got := Singular(plural); got != singular {
			t.Errorf("Singular(%q) = %q, want %q", plural, got, singular)
		}
	}
}
