package freeze_test

import (
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// An unnamed page takes its screen name from its path, and the root is
// "home", so `/` and `/home` both froze to "home" and the generator
// refused the duplicate screen (2026-10-27 K23). A derived name that is
// taken, by a named page or an earlier path, now gains a suffix.
func TestFreezeScreenNamesUnique(t *testing.T) {
	w := world.New()
	tree := world.Node{Kind: "div"}
	for _, p := range []*world.Page{
		{Path: "/", Tree: tree},
		{Path: "/home", Tree: tree},
		{Path: "/home/2", Tree: tree},
		{Path: "/a", Name: "About", Tree: tree},
		{Path: "/about", Tree: tree},
	} {
		w.Pages[p.Path] = p
	}
	buf, err := freeze.AppSpecYAML(w)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := coreyaml.Parse(string(buf))
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, buf)
	}
	got := map[string]string{}
	for _, s := range doc.Map["screens"].List {
		route, name := s.Map["route"].Value.(string), s.Map["name"].Value.(string)
		for r, n := range got {
			if n == name {
				t.Fatalf("%s and %s both froze to screen %q:\n%s", r, route, name, buf)
			}
		}
		got[route] = name
	}
	want := map[string]string{"/": "home", "/home": "home_2", "/home/2": "home_2_2", "/a": "About", "/about": "about_2"}
	for route, name := range want {
		if got[route] != name {
			t.Errorf("screen for %s = %q, want %q", route, got[route], name)
		}
	}
}
