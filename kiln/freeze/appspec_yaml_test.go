package freeze_test

import (
	"encoding/json"
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
		t.Fatalf("no seed list in app spec:\n%s", buf)
	}
	rows := seed.List[0].Map["rows"]
	if rows == nil || rows.Kind != coreyaml.List || len(rows.List) == 0 {
		t.Fatalf("no seed rows in app spec:\n%s", buf)
	}
	return rows.List[0]
}

func TestAppSpecQuotesCommaInList(t *testing.T) {
	w := seedWorld(map[string]any{
		"title": "Ship",
		"tags":  []any{"Unlimited users, unlimited projects", "SSO"},
	})
	buf, err := freeze.AppSpecYAML(w)
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

func TestAppSpecQuotesApostropheInList(t *testing.T) {
	w := seedWorld(map[string]any{
		"title": "Ship",
		"tags":  []any{"it's live", "done"},
	})
	buf, err := freeze.AppSpecYAML(w)
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

func TestAppSpecSeedRowScalarListOnly(t *testing.T) {
	// A row whose only values are scalar lists must still round-trip:
	// the emitter leads the list item with an inline [a, b] key.
	w := seedWorld(map[string]any{"tags": []any{"a", "b"}})
	buf, err := freeze.AppSpecYAML(w)
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

func TestAppSpecRejectsMapOnlySeedRow(t *testing.T) {
	// core/yaml cannot represent a list item whose every value is a nested
	// map; freeze must fail loudly instead of emitting a corrupt app spec.
	w := seedWorld(map[string]any{"settings": map[string]any{"a": 1}})
	_, err := freeze.AppSpecYAML(w)
	if err == nil {
		t.Fatal("expected loud error for map-only seed row, got nil")
	}
	if !strings.Contains(err.Error(), "settings") {
		t.Fatalf("error should name the offending key: %v", err)
	}
}

func TestAppSpecRejectsUnsafeSeedKey(t *testing.T) {
	w := seedWorld(map[string]any{"aria: label": "v"})
	_, err := freeze.AppSpecYAML(w)
	if err == nil {
		t.Fatal("expected loud error for key containing a colon, got nil")
	}
	if !strings.Contains(err.Error(), "aria: label") {
		t.Fatalf("error should name the offending key: %v", err)
	}
}

func TestAppSpecQuotesBareColonScalar(t *testing.T) {
	// A mixed list forces block style; an unquoted "09:30" line would
	// re-parse as the map {09: 30}.
	w := seedWorld(map[string]any{
		"title": "Ship",
		"tags":  []any{"09:30", []any{"a"}},
	})
	buf, err := freeze.AppSpecYAML(w)
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

// An integer past 2^53 reaches the world as a json.Number (the journal
// keeps it exact); the app spec writes it as that number, not as a
// quoted string and not rounded.
func TestAppSpecKeepsBigIntegers(t *testing.T) {
	const exact int64 = 9007199254740993
	big := json.Number("9007199254740993")
	// Past int64 there is no integer to write; it stays the float it was.
	huge := json.Number("100000000000000000000")
	w := seedWorld(map[string]any{"title": big, "tags": []any{big}, "settings": huge})
	buf, err := freeze.AppSpecYAML(w)
	if err != nil {
		t.Fatal(err)
	}
	row := parseSeedRow(t, buf)
	if got := row.Map["title"].Value; got != exact {
		t.Errorf("block scalar = %#v, want int64 %d\n%s", got, exact, buf)
	}
	if got := row.Map["settings"].Value; got != 1e20 {
		t.Errorf("past-int64 scalar = %#v, want float64 1e20\n%s", got, buf)
	}
	if tags := row.Map["tags"]; tags == nil || len(tags.List) != 1 || tags.List[0].Value != exact {
		t.Errorf("flow list item did not round-trip as int64 %d\n%s", exact, buf)
	}
}

// core/yaml refuses a flow map, so an empty JSON object froze as
// `settings: {}` made the whole app spec unparseable. A bare `settings:`
// with nothing indented under it is the empty map core/yaml reads back.
func TestAppSpecEmptyMapRoundTrips(t *testing.T) {
	w := seedWorld(map[string]any{
		"title":    "Ship",
		"settings": map[string]any{},
		"tags":     map[string]any{"inner": map[string]any{}},
	})
	buf, err := freeze.AppSpecYAML(w)
	if err != nil {
		t.Fatal(err)
	}
	row := parseSeedRow(t, buf)
	if s := row.Map["settings"]; s == nil || s.Kind != coreyaml.Map || len(s.Map) != 0 {
		t.Fatalf("settings did not round-trip as an empty map: %#v\n%s", s, buf)
	}
	tags := row.Map["tags"]
	if tags == nil || tags.Kind != coreyaml.Map {
		t.Fatalf("tags did not round-trip as a map:\n%s", buf)
	}
	if in := tags.Map["inner"]; in == nil || in.Kind != coreyaml.Map || len(in.Map) != 0 {
		t.Fatalf("nested empty map did not round-trip: %#v\n%s", in, buf)
	}
}
