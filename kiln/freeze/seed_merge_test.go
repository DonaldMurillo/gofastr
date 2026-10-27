package freeze_test

import (
	"fmt"
	"strings"
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func mergeWorld(seeds ...*world.Seed) *world.World {
	w := world.New()
	for _, name := range []string{"posts", "tags"} {
		w.Entities[name] = &world.Entity{Name: name, Fields: []world.Field{
			{Name: "title", Type: "string"}, {Name: "status", Type: "enum", Values: []string{"draft", "live"}},
		}}
	}
	w.Seeds = seeds
	return w
}

// Each add_seed appends a seed, so a world that seeds one entity twice is
// ordinary, but the generator refuses two seed blocks for one entity (its
// boot hook skips the second once the table has rows). Freeze merges
// them into the first block's place: rows in order, counts summed,
// weights joined.
func TestFreezeMergesSeedsPerEntity(t *testing.T) {
	w := mergeWorld(
		&world.Seed{Entity: "posts", Rows: []map[string]any{{"title": "a"}}, Count: 2},
		&world.Seed{Entity: "tags", Rows: []map[string]any{{"title": "t"}}},
		&world.Seed{Entity: "posts", Rows: []map[string]any{{"title": "b"}}, Count: 3,
			Weights: map[string]map[string]int{"status": {"draft": 1, "live": 3}}},
	)
	buf, err := freeze.AppSpecYAML(w)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := coreyaml.Parse(string(buf))
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, buf)
	}
	seed := doc.Map["seed"]
	if seed == nil || len(seed.List) != 2 {
		t.Fatalf("want 2 seed blocks (posts, tags):\n%s", buf)
	}
	posts := seed.List[0].Map
	if posts["entity"].Value != "posts" || seed.List[1].Map["entity"].Value != "tags" {
		t.Fatalf("blocks out of order:\n%s", buf)
	}
	var titles []string
	for _, row := range posts["rows"].List {
		titles = append(titles, fmt.Sprint(row.Map["title"].Value))
	}
	if strings.Join(titles, ",") != "a,b" {
		t.Errorf("merged rows = %v, want a,b", titles)
	}
	if posts["count"] == nil || fmt.Sprint(posts["count"].Value) != "5" {
		t.Errorf("merged count missing or not 5:\n%s", buf)
	}
	if wt := posts["weights"]; wt == nil || wt.Map["status"] == nil || fmt.Sprint(wt.Map["status"].Map["live"].Value) != "3" {
		t.Errorf("merged weights lost the second seed's status weights:\n%s", buf)
	}
}

// Two seeds weighting the same column differently have no merge that
// keeps both, so freeze refuses them by name rather than pick one.
func TestFreezeRefusesWeightConflict(t *testing.T) {
	w := mergeWorld(
		&world.Seed{Entity: "posts", Count: 2, Weights: map[string]map[string]int{"status": {"draft": 1}}},
		&world.Seed{Entity: "posts", Count: 2, Weights: map[string]map[string]int{"status": {"live": 1}}},
	)
	_, err := freeze.AppSpecYAML(w)
	if err == nil || !strings.Contains(err.Error(), `"status"`) {
		t.Fatalf("conflicting weights = %v, want a refusal naming the column", err)
	}
	same := mergeWorld(
		&world.Seed{Entity: "posts", Count: 2, Weights: map[string]map[string]int{"status": {"draft": 1}}},
		&world.Seed{Entity: "posts", Count: 2, Weights: map[string]map[string]int{"status": {"draft": 1}}},
	)
	if _, err := freeze.AppSpecYAML(same); err != nil {
		t.Fatalf("identical weights refused: %v", err)
	}
}
