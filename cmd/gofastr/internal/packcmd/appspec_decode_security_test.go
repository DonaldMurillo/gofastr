package packcmd

import (
	"testing"
)

// Property: a value the decoder cannot read must be an error, not a silent
// zero-default — specifically for app.db, where the silent default is "no
// database".
//
// app.db.driver and app.db.url decode through stringValue, which returns ""
// for a non-scalar node. A driver written as a nested map (an agent merging
// fragments, or a YAML anchor gone wrong) therefore decodes to "" and the
// generated openDB takes its `driver == "" && dbURL == ""` branch: the app
// ships with NO database, silently — every CRUD screen 500s at runtime and,
// per the seed fail-fast contract, any seed data aborts boot. The theme
// decoder already built requireScalarString for exactly this shape
// ("app.theme primary: non-scalar" is a decode error); db is the sharper
// surface and lacks the guard.
func TestDBConfigRejectsNonScalarTypes(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"driver as map", "app:\n  name: X\n  db:\n    driver:\n      nested: true\n"},
		{"driver as list", "app:\n  name: X\n  db:\n    driver: [sqlite]\n"},
		{"url as map", "app:\n  name: X\n  db:\n    url:\n      host: h\n"},
		{"url as list", "app:\n  name: X\n  db:\n    url: [a, b]\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeAppSpecString(tc.yaml)
			if err == nil {
				t.Fatalf("SECURITY: [decode] %s decoded to the empty string; the app silently generates with no database:\n%s", tc.name, tc.yaml)
			}
		})
	}
	// Control: scalar driver/url still decode.
	bp, err := decodeAppSpecString("app:\n  name: X\n  db:\n    driver: sqlite\n    url: file:x.db\n")
	if err != nil {
		t.Fatalf("scalar db config must decode: %v", err)
	}
	if bp.App.DBDriver != "sqlite" || bp.App.DBURL != "file:x.db" {
		t.Fatalf("scalar db config lost: %+v", bp.App)
	}
}
