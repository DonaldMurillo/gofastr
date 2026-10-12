package upgrade_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scantest"
)

// The blueprint-keys note, through the shipped YAML: each removed key and
// each removed block kind in a gofastr.yml is a hit on its own, and a
// blueprint on the current spellings is silent.
func TestV087BlueprintNoteHitsRemovedKinds(t *testing.T) {
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("upgrade.Load: %v", err)
	}
	i := slices.IndexFunc(reg.Releases, func(r upgrade.Release) bool { return r.Version == "v0.87.0" })
	if i < 0 {
		t.Fatal("registry has no v0.87.0")
	}
	var n *upgrade.Note
	for _, c := range reg.Releases[i].Notes {
		if strings.HasPrefix(c.Change, "the blueprint's screen-level") {
			n = scantest.Only(c, "text")
		}
	}
	if n == nil {
		t.Fatal("v0.87.0 has no blueprint-keys note")
	}
	screen := func(body string) map[string]string {
		return map[string]string{"gofastr.yml": "screens:\n  - route: /notes\n    body:\n" + body}
	}
	for name, body := range map[string]string{
		"filters":       "      - kind: entity_list\n        entity: notes\n        filters:\n          - status\n",
		"entity_create": "      - kind: entity_create\n        entity: notes\n",
		"entity_edit":   "      - kind: entity_edit\n        entity: notes\n",
		"quoted kind":   "      - kind: \"entity_edit\"\n        entity: notes\n",
		"nested kind":   "      - kind: section\n        blocks:\n          - kind: entity_create\n            entity: notes\n",
	} {
		app := scantest.App(t, screen(body), scantest.Options{})
		if got := scantest.Hits(scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) != 1 {
			t.Errorf("%s: hits = %v, want one", name, got)
		}
	}
	quiet := scantest.App(t, screen("      - kind: entity_list\n        entity: notes\n        create: true\n      - kind: entity_detail\n        entity: notes\n      - kind: text\n        text: entity_create is gone\n"), scantest.Options{})
	if got := scantest.Hits(scantest.Run(t, quiet, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) != 0 {
		t.Fatalf("fires on a current blueprint: %v", got)
	}
}
