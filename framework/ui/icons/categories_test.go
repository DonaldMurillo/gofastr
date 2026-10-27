package icons

import (
	"strings"
	"testing"
)

func TestCategoriesCoverEveryDrawingOnce(t *testing.T) {
	seen := map[string]bool{}
	ids := map[string]bool{}
	for _, group := range categoryGroups {
		if group.ID == "" || group.Label == "" || ids[group.ID] {
			t.Fatalf("empty or duplicate category: %+v", group.Category)
		}
		ids[group.ID] = true
		members := strings.Fields(group.names)
		if len(members) == 0 {
			t.Errorf("category %q is empty", group.ID)
		}
		for _, name := range members {
			if seen[name] {
				t.Errorf("%q belongs to more than one category", name)
			}
			seen[name] = true
			if _, ok := bodies[name]; !ok {
				t.Errorf("%q in category %q is not a canonical icon", name, group.ID)
			}
		}
	}
	for _, name := range Names() {
		if !seen[name] {
			t.Errorf("%q has no category", name)
		}
	}
	for alias, target := range Aliases() {
		got, ok := CategoryFor(alias)
		want, _ := CategoryFor(target)
		if !ok || got != want {
			t.Errorf("alias %q category = %+v, want %+v", alias, got, want)
		}
	}
	if _, ok := CategoryFor("missing-icon"); ok {
		t.Error("unknown icon has a category")
	}
	categories := Categories()
	categories[0].Label = "changed"
	if Categories()[0].Label == "changed" {
		t.Error("Categories exposes shared state")
	}
}
