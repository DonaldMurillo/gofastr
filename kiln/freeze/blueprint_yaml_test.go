package freeze_test

import (
	"strings"
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func seedWorld(rows ...map[string]any) *world.World {
	w := world.New()
	w.Entities["posts"] = &world.Entity{
		Name: "posts",
		Fields: []world.Field{
			{Name: "title", Type: "string"},
			{Name: "settings", Type: "json"},
			{Name: "tags", Type: "json"},
		},
	}
	w.Seeds = []*world.Seed{{Entity: "posts", Rows: rows}}
	return w
}

func parseSeedRow(t *testing.T, buf []byte) *coreyaml.Node {
	t.Helper()
	doc, err := coreyaml.Parse(string(buf))
	if err != nil {
		t.Fatalf("core/yaml rejected freeze output: %v\n%s", err, buf)
	}
	seed := doc.Map["seed"]
	if seed == nil || seed.Kind != coreyaml.List || len(seed.List) == 0 {
		t.Fatalf("no seed list in blueprint:\n%s", buf)
	}
	rows := seed.List[0].Map["rows"]
	if rows == nil || rows.Kind != coreyaml.List || len(rows.List) == 0 {
		t.Fatalf("no seed rows in blueprint:\n%s", buf)
	}
	return rows.List[0]
}

func TestBlueprintQuotesCommaInList(t *testing.T) {
	w := seedWorld(map[string]any{
		"title": "Ship",
		"tags":  []any{"Unlimited users, unlimited projects", "SSO"},
	})
	buf, err := freeze.BlueprintYAML(w)
	if err != nil {
		t.Fatal(err)
	}
	row := parseSeedRow(t, buf)
	tags := row.Map["tags"]
	if tags == nil || tags.Kind != coreyaml.List {
		t.Fatalf("tags did not round-trip as a list:\n%s", buf)
	}
	if len(tags.List) != 2 {
		t.Fatalf("comma split the list: got %d items, want 2\n%s", len(tags.List), buf)
	}
	if got := tags.List[0].Value; got != "Unlimited users, unlimited projects" {
		t.Fatalf("first tag = %q", got)
	}
}

func TestBlueprintQuotesApostropheInList(t *testing.T) {
	w := seedWorld(map[string]any{
		"title": "Ship",
		"tags":  []any{"it's live", "done"},
	})
	buf, err := freeze.BlueprintYAML(w)
	if err != nil {
		t.Fatal(err)
	}
	row := parseSeedRow(t, buf)
	tags := row.Map["tags"]
	if tags == nil || tags.Kind != coreyaml.List || len(tags.List) != 2 {
		t.Fatalf("apostrophe list did not round-trip:\n%s", buf)
	}
	if got := tags.List[0].Value; got != "it's live" {
		t.Fatalf("first tag = %q", got)
	}
}

func TestBlueprintSeedRowScalarListOnly(t *testing.T) {
	// A row whose only values are scalar lists must still round-trip:
	// the emitter leads the list item with an inline [a, b] key.
	w := seedWorld(map[string]any{"tags": []any{"a", "b"}})
	buf, err := freeze.BlueprintYAML(w)
	if err != nil {
		t.Fatal(err)
	}
	row := parseSeedRow(t, buf)
	tags := row.Map["tags"]
	if tags == nil || tags.Kind != coreyaml.List || len(tags.List) != 2 {
		t.Fatalf("scalar-list row did not round-trip:\n%s", buf)
	}
	if len(row.Map) != 1 {
		t.Fatalf("row grew phantom keys: %d keys, want 1\n%s", len(row.Map), buf)
	}
}

func TestBlueprintRejectsMapOnlySeedRow(t *testing.T) {
	// core/yaml cannot represent a list item whose every value is a nested
	// map; freeze must fail loudly instead of emitting a corrupt blueprint.
	w := seedWorld(map[string]any{"settings": map[string]any{"a": 1}})
	_, err := freeze.BlueprintYAML(w)
	if err == nil {
		t.Fatal("expected loud error for map-only seed row, got nil")
	}
	if !strings.Contains(err.Error(), "settings") {
		t.Fatalf("error should name the offending key: %v", err)
	}
}

func TestBlueprintRejectsUnsafeSeedKey(t *testing.T) {
	w := seedWorld(map[string]any{"aria: label": "v"})
	_, err := freeze.BlueprintYAML(w)
	if err == nil {
		t.Fatal("expected loud error for key containing a colon, got nil")
	}
	if !strings.Contains(err.Error(), "aria: label") {
		t.Fatalf("error should name the offending key: %v", err)
	}
}

func TestBlueprintQuotesBareColonScalar(t *testing.T) {
	// A mixed list forces block style; an unquoted "09:30" line would
	// re-parse as the map {09: 30}.
	w := seedWorld(map[string]any{
		"title": "Ship",
		"tags":  []any{"09:30", []any{"a"}},
	})
	buf, err := freeze.BlueprintYAML(w)
	if err != nil {
		t.Fatal(err)
	}
	row := parseSeedRow(t, buf)
	tags := row.Map["tags"]
	if tags == nil || tags.Kind != coreyaml.List || len(tags.List) != 2 {
		t.Fatalf("mixed list did not round-trip:\n%s", buf)
	}
	if got := tags.List[0].Value; got != "09:30" {
		t.Fatalf("colon scalar corrupted: got %v (kind %v)\n%s", got, tags.List[0].Kind, buf)
	}
}
