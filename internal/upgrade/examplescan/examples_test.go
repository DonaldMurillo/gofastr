package examplescan

import (
	"errors"
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
		var err error
		r.notes, err = exampleNotes(reg.Releases[at].Notes)
		if err != nil {
			t.Fatalf("%s: %v", r.version, err)
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

func exampleNotes(notes []*upgrade.Note) ([]*upgrade.Note, error) {
	var kept []*upgrade.Note
	for _, note := range notes {
		narrowed := scantest.Only(note, "strings", "css", "config", "text")
		if !narrowed.Find.Empty() {
			kept = append(kept, narrowed)
		}
	}
	if len(kept) == 0 {
		return nil, errors.New("release has no strings, CSS, config or text matchers after narrowing")
	}
	return kept, nil
}

func TestExamplesNeedEffectiveMatchers(t *testing.T) {
	apiOnly := &upgrade.Note{Find: upgrade.Find{Uses: []upgrade.Symbol{{Pkg: "example.test/lib", Name: "Old"}}}}
	for _, notes := range [][]*upgrade.Note{nil, {apiOnly}, {{Nodetect: "No source spelling changes"}}} {
		if _, err := exampleNotes(notes); err == nil {
			t.Errorf("notes %+v passed without an effective matcher", notes)
		}
	}
	for _, find := range []upgrade.Find{
		{Strings: upgrade.StringMatch{Classes: []string{"old"}}},
		{CSS: upgrade.CSSMatch{Classes: []string{"old"}}},
		{Config: []upgrade.ConfigMatch{{Key: "old"}}},
		{Text: []upgrade.TextMatch{{Glob: "*.js"}}},
	} {
		note := &upgrade.Note{Find: find}
		kept, err := exampleNotes([]*upgrade.Note{apiOnly, note})
		if err != nil || len(kept) != 1 || kept[0].Find.Empty() {
			t.Errorf("effective matcher %+v was lost: kept=%+v err=%v", find, kept, err)
		}
		if note.Find.Empty() || apiOnly.Find.Empty() {
			t.Error("narrowing changed the source notes")
		}
	}
}
