package semantic

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
)

// assertMetaMatchIsEquality pins Filter.MetaMatch to its documented
// contract, per key equality, on any Store. PgVectorStore's JSONB
// containment let {"tenants": []} match every chunk holding an array (a
// scope filter that scoped nothing), and FlatStore's reflect.DeepEqual
// told Go int metadata from the float64 a JSON filter decodes to.
func assertMetaMatchIsEquality(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()
	if err := s.Add(ctx, []Chunk{
		{ID: "a", DocID: "d1", Text: "x", Vec: vec3(1, 0, 0), Metadata: map[string]any{"tenants": []any{"acme"}, "n": 5, "kind": "doc", "cfg": map[string]any{"n": 5}}},
		{ID: "b", DocID: "d2", Text: "y", Vec: vec3(0, 1, 0), Metadata: map[string]any{"tenants": []any{}, "n": 7}},
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		match map[string]any
		want  []string
	}{
		{"empty array is not a wildcard", map[string]any{"tenants": []any{}}, []string{"b"}},
		{"array equality", map[string]any{"tenants": []any{"acme"}}, []string{"a"}},
		{"json float equals go int", map[string]any{"n": 5.0}, []string{"a"}},
		{"go int equals go int", map[string]any{"n": 7}, []string{"b"}},
		{"null needs the key", map[string]any{"missing": nil}, nil},
		{"number spelling is not value", map[string]any{"n": json.Number("5.0")}, []string{"a"}},
		{"nested number spelling", map[string]any{"cfg": map[string]any{"n": json.Number("5.0")}}, []string{"a"}},
		{"named string type", map[string]any{"kind": metaKind("doc")}, []string{"a"}},
	} {
		hits, err := s.Candidates(ctx, vec3(1, 0, 0), Filter{MetaMatch: tc.match}, 10)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		var got []string
		for _, h := range hits {
			got = append(got, h.Chunk.ID)
		}
		slices.Sort(got)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: MetaMatch=%v matched %v, want %v", tc.name, tc.match, got, tc.want)
		}
	}
}

// metaKind is a host's named string type, which encodes as a JSON string.
type metaKind string

func TestMetaMatchIsEqualityFlat(t *testing.T) {
	assertMetaMatchIsEquality(t, NewFlatStore(3, "test"))
}

func TestMetaMatchIsEqualityPgVector(t *testing.T) {
	assertMetaMatchIsEquality(t, newPgStore(t, 3))
}
