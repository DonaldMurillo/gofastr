package render

import (
	"testing"
	"uuid"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// StampSeed writes the values kiln/live journals with a seed: the
// implicit id and declared auto fields, never the implicit timestamps
// (the app spec generator refuses a seed key that names no field), and
// it leaves a given value, an empty row and the caller's seed alone.
func TestStampSeedWritesJournaledFields(t *testing.T) {
	ents := map[string]*world.Entity{"posts": {Name: "posts", Fields: []world.Field{
		{Name: "title", Type: "string"},
		{Name: "token", Type: "uuid", AutoGenerate: "uuid"},
		{Name: "published_at", Type: "timestamp", AutoGenerate: "timestamp"},
	}}}
	in := &world.Seed{Entity: "posts", Rows: []map[string]any{
		{"title": "a"},
		{"title": "b", "ID": "p-2", "token": nil},
		{},
	}}
	out, err := StampSeed(ents, in)
	if err != nil {
		t.Fatal(err)
	}
	a := out.Rows[0]
	if _, err := uuid.Parse(a["id"].(string)); err != nil {
		t.Errorf("row 1 id = %v, want a generated UUID", a["id"])
	}
	if _, err := uuid.Parse(a["token"].(string)); err != nil {
		t.Errorf("row 1 token = %v, want a generated UUID", a["token"])
	}
	if s, _ := a["published_at"].(string); s == "" {
		t.Errorf("row 1 published_at = %v, want a timestamp", a["published_at"])
	}
	for _, k := range []string{"created_at", "updated_at"} {
		if _, ok := a[k]; ok {
			t.Errorf("row 1 carries the implicit %s, which the generator refuses in a seed", k)
		}
	}
	b := out.Rows[1]
	if b["ID"] != "p-2" {
		t.Errorf("row 2 ID = %v, want the given p-2", b["ID"])
	}
	if _, ok := b["id"]; ok {
		t.Error("row 2 got a second id beside the given ID")
	}
	if b["token"] == nil {
		t.Error("row 2 null token was not filled")
	}
	if len(out.Rows[2]) != 0 {
		t.Errorf("an empty row was filled: %v", out.Rows[2])
	}
	if _, ok := in.Rows[0]["id"]; ok {
		t.Error("StampSeed changed the caller's seed")
	}
}
