package examplescan

import (
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scantest"
)

// TestExamplesStaySilent scans the repo's examples tree (migrated code)
// once with every v0.86.0 and v0.87.0 note's string, css, config and
// text matchers, then asserts per release: zero hits. Go-API matchers
// are stripped: a renamed symbol's replacement still exists at HEAD and
// the examples call it, which is correct for an app below the release
// and not a property of migrated code. The kept matchers record hits per
// note and read nothing another note found, so one scan carrying both
// releases' notes reports each note exactly as a scan of its release
// alone would.
//
//gofastr:test-reads examples/
func TestExamplesStaySilent(t *testing.T) {
	reg := scantest.Registry(t)
	releases := []struct {
		version string
		// typeChecked refuses zero hits from a tree that did not
		// type-check: the attribute matcher needs the kit's Attrs type
		// to see map keys.
		typeChecked bool
		notes       []*upgrade.Note
	}{
		{version: "v0.86.0"},
		{version: "v0.87.0", typeChecked: true},
	}
	var all []*upgrade.Note
	for i := range releases {
		r := &releases[i]
		at := slices.IndexFunc(reg.Releases, func(rel upgrade.Release) bool { return rel.Version == r.version })
		if at < 0 {
			t.Fatalf("no %s release in the registry", r.version)
		}
		for _, n := range reg.Releases[at].Notes {
			r.notes = append(r.notes, scantest.Only(n, "strings", "css", "config", "text"))
		}
		all = append(all, r.notes...)
	}
	res := scantest.Run(t, "../../../examples", all, reg.MarkerSinks)
	for _, r := range releases {
		t.Run(r.version, func(t *testing.T) {
			if r.typeChecked && (!res.TypeChecked || len(res.Broken) != 0) {
				t.Fatalf("examples tree did not type-check: broken=%v errors=%+v", res.Broken, res.Unexplained)
			}
			for i, n := range r.notes {
				for _, h := range scantest.Hits(res, n) {
					t.Errorf("%s note %d (%s): fires on the migrated examples tree: %s", r.version, i, n.Change, h)
				}
			}
		})
	}
}
