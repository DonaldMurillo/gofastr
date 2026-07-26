package crud

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A WireName has to hold in BOTH directions and under BOTH casings: the
// response encoder renames the column to the alias, and the request decoder
// resolves the alias back to the column. If either direction misses, a client
// writes one field and reads another.
func TestWireName_RoundTripsBothCasings(t *testing.T) {
	ent := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "body_text", Type: schema.String, WireName: "content"},
			{Name: "author_id", Type: schema.Int}, // no alias — case conversion only
		},
	}.WithTimestamps(false))

	for _, jc := range []JSONCase{CaseCamel, CaseSnake} {
		ch := &CrudHandler{Entity: ent, JSONCase: jc}

		// Out: the aliased column takes its wire name under either casing;
		// the un-aliased one follows the handler's casing.
		out := ch.convertMapKeys(map[string]any{"body_text": "x", "author_id": 1})
		if _, ok := out["content"]; !ok {
			t.Errorf("case %v: response key should be the alias %q, got keys %v",
				jc, "content", wireKeysOf(out))
		}
		if _, ok := out["body_text"]; ok {
			t.Errorf("case %v: response leaked the raw column name", jc)
		}

		// Back in: the alias resolves to the column.
		in := ch.unconvertMapKeys(map[string]any{"content": "y"})
		if _, ok := in["body_text"]; !ok {
			t.Errorf("case %v: request alias did not resolve to the column, got %v",
				jc, wireKeysOf(in))
		}

		// A key that matches nothing passes through rather than vanishing —
		// dropping it silently would hide a client's typo.
		if got := ch.unconvertMapKeys(map[string]any{"totally_unknown": 1}); len(got) != 1 {
			t.Errorf("case %v: unknown key was dropped instead of passed through", jc)
		}
	}
}
