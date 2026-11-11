package appspec

import (
	"strings"
	"testing"
)

// The generated app serves app.static_dir to anyone who asks. A
// static_dir that is the project root (or above it) served main.go,
// go.mod, the built binary and the live SQLite database; one over a
// directory the generator writes Go into served that source, and one
// holding the database file served every row.
func TestStaticDirRefusesProjectFiles(t *testing.T) {
	for _, c := range []struct{ dir, db string }{
		{".", ""},
		{"./", ""},
		{"", ""}, // control: no static dir
		{"..", ""},
		{"../..", ""},
		{"/", ""},
		{"entities", ""},
		{"./entities/client", ""},
		{"cmd", ""},
		{"gen/js", ""},
		{"siteheader", ""},
		{"docpage", ""},
		{"data", "file:data/app.db"},
		{"data", "data/app.db?_pragma=busy_timeout(5000)"},
		{"./data/", "file:./data/app.db"},
	} {
		bp := AppSpec{}
		bp.App.StaticDir = c.dir
		bp.App.DBURL = c.db
		err := ValidateAppSpec(bp)
		refused := err != nil && strings.Contains(err.Error(), "static_dir")
		if c.dir == "" {
			if refused {
				t.Errorf("no static_dir refused: %v", err)
			}
			continue
		}
		if !refused {
			t.Errorf("static_dir %q (db %q) accepted: %v", c.dir, c.db, err)
		}
	}
	for _, c := range []struct{ dir, db string }{
		{"static", ""},
		{"static", "file:app.db"},
		{"public/assets", "file:data/app.db"},
		{"../shared/static", ""},
		{"/srv/www", "file:app.db"},
		{"static", "postgres://u@h/db"},
		{"datastatic", "file:data/app.db"},
	} {
		bp := AppSpec{}
		bp.App.StaticDir = c.dir
		bp.App.DBURL = c.db
		if err := ValidateAppSpec(bp); err != nil && strings.Contains(err.Error(), "static_dir") {
			t.Errorf("static_dir %q (db %q) refused: %v", c.dir, c.db, err)
		}
	}
}

// With app.output_dir the generator owns that directory outright (the
// ecommerce layout, output_dir: app), so a static_dir at it, inside it
// or above it served the generated main.go, entities/ and cmd/.
func TestStaticDirRefusesOutputDir(t *testing.T) {
	for _, c := range []struct {
		dir, out string
		refused  bool
	}{
		{"app", "app", true},
		{"./app/", "app", true},
		{"app/public", "app", true},
		{"src", "src/app", true},
		{"static", "app", false},
		{"apps", "app", false},
		{"app", "", false},
		{"app", ".", false},
	} {
		bp := AppSpec{}
		bp.App.StaticDir = c.dir
		bp.App.OutputDir = c.out
		err := ValidateAppSpec(bp)
		got := err != nil && strings.Contains(err.Error(), "static_dir")
		if got != c.refused {
			t.Errorf("static_dir %q with output_dir %q: refused=%v, want %v (%v)", c.dir, c.out, got, c.refused, err)
		}
	}
}
