package semantic

import (
	"context"
	"testing"
)

// The pgvector store runs under any configured table name: a mixed-case
// Chunks keeps its quoted spelling throughout, and a table named excluded
// does not collide with the EXCLUDED pseudo-row of the chunk upsert.
func TestPgVectorConfiguredTableNames(t *testing.T) {
	for _, table := range []string{"Chunks", "excluded"} {
		t.Run(table, func(t *testing.T) {
			db := openPgVector(t)
			ctx := context.Background()
			s, err := NewPgVector(db, PgVectorConfig{Dim: 3, Model: "test", Table: table})
			if err != nil {
				t.Fatalf("NewPgVector: %v", err)
			}
			if err := s.EnsureSchema(ctx); err != nil {
				t.Fatalf("EnsureSchema: %v", err)
			}
			c := Chunk{ID: "c1", DocID: "d1", Text: "first", Vec: vec3(1, 0, 0)}
			if err := s.Add(ctx, []Chunk{c}); err != nil {
				t.Fatalf("add: %v", err)
			}
			c.Text = "second"
			if err := s.Add(ctx, []Chunk{c}); err != nil {
				t.Fatalf("upsert over an existing id: %v", err)
			}
			got, ok := s.ChunkByID("c1")
			if !ok || got.Text != "second" {
				t.Errorf("chunk after upsert = %+v (found %v), want text second", got, ok)
			}
			if ids := s.ChunkIDsForDoc("d1"); len(ids) != 1 {
				t.Errorf("chunks for d1 = %v, want one", ids)
			}
		})
	}
}
