package crud

import (
	"context"
	"testing"
)

// A nil body on a typed write acts as an empty map: no panic, and the
// write gives the result an empty body gives.
func TestTypedWritesTreatNilBodyAsEmpty(t *testing.T) {
	ch, db := setupCamelDocsHandler(t)
	if _, err := db.Exec(`INSERT INTO reddocs (id, body_text) VALUES ('d1','orig')`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		write func(context.Context, map[string]any) error
	}{
		{"create", func(ctx context.Context, b map[string]any) error { _, err := ch.CreateOne(ctx, b); return err }},
		{"upsert", func(ctx context.Context, b map[string]any) error { _, err := ch.UpsertOne(ctx, b); return err }},
		{"batch create", func(ctx context.Context, b map[string]any) error {
			_, err := ch.BatchCreateMany(ctx, []map[string]any{b})
			return err
		}},
		{"update", func(ctx context.Context, b map[string]any) error { _, err := ch.UpdateOne(ctx, "d1", b); return err }},
		{"batch update", func(ctx context.Context, b map[string]any) error {
			_, err := ch.BatchUpdateMany(ctx, []string{"d1"}, []map[string]any{b})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.write(t.Context(), map[string]any{})
			var got error
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("nil body panicked: %v", r)
					}
				}()
				got = tc.write(t.Context(), nil)
			}()
			if (got == nil) != (want == nil) {
				t.Errorf("nil body err = %v, empty body err = %v", got, want)
			}
		})
	}
}
