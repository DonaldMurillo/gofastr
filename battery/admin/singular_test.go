package admin

import "testing"

// Headings and buttons read "New category", not "New categorie".
func TestSingularInflectsEntityNames(t *testing.T) {
	for in, want := range map[string]string{
		"categories": "category",
		"statuses":   "status",
		"boxes":      "box",
		"users":      "user",
		"posts":      "post",
		"address":    "address",
	} {
		if got := singular(in); got != want {
			t.Errorf("singular(%q) = %q, want %q", in, got, want)
		}
	}
}
