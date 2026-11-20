package world

import "testing"

func TestValidateRoutePattern(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"/users/{id}", ""},
		{"", `route "GET" "": path must start with /`},
		{"/{wx", `route "GET" "/{wx": parsing "GET /{wx": at offset 5: bad wildcard segment (must end with '}')`},
	} {
		err := ValidateRoute(&Route{Method: "GET", Path: tc.path})
		if tc.want == "" {
			if err != nil {
				t.Errorf("path %q: %v", tc.path, err)
			}
		} else if err == nil || err.Error() != tc.want {
			t.Errorf("path %q: error = %v, want %q", tc.path, err, tc.want)
		}
	}
}

func TestOverlappingPatternValidation(t *testing.T) {
	for _, tc := range []struct{ existing, pattern, want string }{
		{"GET /users/{id}", "GET /users/{name}", "GET /users/{id}"},
		{"GET /users", "POST /users", ""},
		{"GET /{wx", "GET /users", ""},
		{"", "GET /users", ""},
		{"/users", "GET /users", ""},
		{"/users", "/users", "/users"},
		{"GET\t/users", "GET /users", "GET\t/users"},
	} {
		if got := OverlappingPattern([]string{tc.existing}, tc.pattern); got != tc.want {
			t.Errorf("OverlappingPattern(%q, %q) = %q, want %q", tc.existing, tc.pattern, got, tc.want)
		}
	}
}

// A node with two forbidden props is refused naming the same one on every
// call: ValidatePageTree ranged the props map, so the refusal named
// whichever key the iteration reached first.
func TestValidatePageTreeRefusalIsDeterministic(t *testing.T) {
	n := Node{Kind: "div", Props: map[string]any{"class": "a", "style": "b", "onclick": "c", "onload": "d"}}
	first := ValidatePageTree(n)
	if first == nil {
		t.Fatal("styling props accepted")
	}
	for range 64 {
		if err := ValidatePageTree(n); err == nil || err.Error() != first.Error() {
			t.Fatalf("refusal changed between calls: %q then %v", first, err)
		}
	}
}
