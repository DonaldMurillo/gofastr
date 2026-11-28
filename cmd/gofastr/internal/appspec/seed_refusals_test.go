package appspec

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

func TestSeedRowsSkipServerColumnsAndMixEnums(t *testing.T) {
	decl := framework.EntityDeclaration{Name: "orders", Fields: []framework.FieldDeclaration{
		{Name: "title", Type: "string"},
		{Name: "secret", Type: "string", Hidden: true},
		{Name: "total", Type: "int", ReadOnly: true},
		{Name: "code", Type: "string", AutoGenerate: "uuid"},
		{Name: "created_at", Type: "timestamp"},
		{Name: "tenant_id", Type: "string"},
		{Name: "status", Type: "enum", Values: []string{"open", "closed"}},
	}}
	rows := AppSpecGenerateSeedRows(decl, 6, map[string]map[string]int{"status": {"open": 1, "closed": 1}})
	for _, row := range rows {
		for _, col := range []string{"secret", "total", "code", "created_at", "tenant_id"} {
			if _, ok := row[col]; ok {
				t.Fatalf("generated row sets %s, which the server fills or hides: %v", col, row)
			}
		}
	}
	if rows[0]["status"] == rows[1]["status"] {
		t.Fatalf("enum values are grouped, not interleaved: %v then %v", rows[0]["status"], rows[1]["status"])
	}
}

// Two values whose default weights collide must still not split evenly.
func TestSeedDefaultSkewSurvivesEqualWeights(t *testing.T) {
	other := ""
	for i := 0; i < 1000 && other == ""; i++ {
		v := fmt.Sprintf("b%d", i)
		if appSpecSeedHash("orders|status|a")%7 == appSpecSeedHash("orders|status|"+v)%7 {
			other = v
		}
	}
	counts := map[string]int{}
	for _, v := range appSpecEnumDistribution("orders", "status", []string{"a", other}, nil, 10) {
		counts[v]++
	}
	if counts["a"] == counts[other] {
		t.Fatalf("equal default weights split 10 rows evenly: %v", counts)
	}
}

func TestSeedUUIDKeyTakesOneValuePerRow(t *testing.T) {
	decl := framework.EntityDeclaration{Name: "refs", PrimaryKey: []string{"ref"}, Fields: []framework.FieldDeclaration{{Name: "ref", Type: "uuid", Required: true}}}
	seen := map[any]bool{}
	for _, row := range AppSpecGenerateSeedRows(decl, 3, nil) {
		if seen[row["ref"]] {
			t.Fatalf("uuid key %v repeats", row["ref"])
		}
		seen[row["ref"]] = true
	}
}

// Each case is a seed the load refuses, with a phrase its error holds.
func TestSeedRefusals(t *testing.T) {
	const notes = "  - name: notes\n    fields:\n      - name: title\n        type: string\n"
	for _, tc := range []struct {
		name, entities, seed, want string
	}{
		{"a seed names its entity", notes, "  - rows:\n      - title: a\n", "entity is required"},
		{"a count is not negative", notes, "  - entity: notes\n    count: -1\n", "count must be >= 0"},
		{"a weight is not negative", notes, "  - entity: notes\n    count: 2\n    weights:\n      title:\n        a: -1\n", "must be >= 0"},
		{"a seed names a declared entity", notes, "  - entity: nope\n    rows:\n      - title: a\n", "seed targets unknown entity"},
		{"a count cannot fill a date key", "  - name: days\n    primary_key: [day]\n    fields:\n      - name: day\n        type: date\n        required: true\n", "  - entity: days\n    count: 2\n", "cannot fill the key column"},
		{"a count repeats no seeded key", "  - name: slots\n    primary_key: [n]\n    fields:\n      - name: n\n        type: int\n        required: true\n", "  - entity: slots\n    rows:\n      - n: 1001\n    count: 1\n", "already holds"},
		{"a count stays inside the key's bounds", "  - name: slots\n    primary_key: [n]\n    fields:\n      - name: n\n        type: int\n        required: true\n        max: 2\n", "  - entity: slots\n    count: 3\n", "which startup refuses"},
		{"a count does not fabricate a required relation", notes + "  - name: comments\n    fields:\n      - name: body\n        type: string\n      - name: note\n        type: relation\n        to: notes\n        required: true\n", "  - entity: comments\n    count: 2\n", "cannot fabricate the required relation"},
		{"a count fits an enum key", "  - name: levels\n    primary_key: [level]\n    fields:\n      - name: level\n        type: enum\n        values: [low, high]\n        required: true\n", "  - entity: levels\n    count: 2\n", "exceeds its key"},
		{"a reference points backwards", notes + "  - name: comments\n    fields:\n      - name: body\n        type: string\n      - name: note\n        type: relation\n        to: notes\n", "  - entity: comments\n    rows:\n      - body: hi\n        note: \"@notes.title=a\"\n  - entity: notes\n    rows:\n      - title: a\n", "is seeded later"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bp, err := decodeSpecString(t, "app:\n  name: a\n  module: example.com/a\nentities:\n"+tc.entities+"seed:\n"+tc.seed)
			if err == nil {
				err = ValidateAppSpec(bp)
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("seed error = %v, want one containing %q", err, tc.want)
			}
		})
	}
}
