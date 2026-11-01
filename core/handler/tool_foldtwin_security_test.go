package handler

import (
	"context"
	"errors"
	"testing"
)

// Pins: ToolAdapter refuses two params keys that fold to one name across the
// WHOLE params object, before it splits the object into the tagged half and
// the body half.
// Finding (2026-11 hunt, H8): for `Q string query:"q"` (no json tag), params
// {"q":"safe","Q":"danger"} succeeded. The tag half bound "safe" from "q";
// the body half held only "Q", which passed its own strict-key check (it is
// the Go field name encoding/json falls back to) and overwrote the field. The
// handler ran "danger" while a reviewer or log reading params["q"] saw "safe".

type foldTwinInput struct {
	Q     string `query:"q"`
	Limit int    `json:"limit"`
}

func echoFoldTwin(_ context.Context, in foldTwinInput) (foldTwinInput, error) { return in, nil }

func TestToolAdapterRefusesFoldedTwinAcrossHalves(t *testing.T) {
	cases := []map[string]any{
		{"q": "safe", "Q": "danger"},
		{"Q": "danger", "q": "safe"},
		{"q": "safe", "Q": "danger", "limit": float64(5)},
		{"limit": float64(5), "LIMIT": float64(9)},
	}
	for _, params := range cases {
		out, err := ToolAdapter(Handler[foldTwinInput, foldTwinInput](echoFoldTwin))(context.Background(), params)
		if err == nil {
			t.Errorf("SECURITY: [tool-foldtwin] params %v accepted, handler saw %+v: a tagged "+
				"key and its case-folded twin read two ways", params, out)
			continue
		}
		var he *Error
		if !errors.As(err, &he) || he.Code != 400 || he.Reason.Code() != ReasonJSONFoldedKeys.Code() {
			t.Errorf("params %v: want 400 %s, got %v", params, ReasonJSONFoldedKeys.Code(), err)
		}
	}
}

func TestToolAdapterFoldCheckKeepsLegitCalls(t *testing.T) {
	out, err := ToolAdapter(Handler[foldTwinInput, foldTwinInput](echoFoldTwin))(context.Background(),
		map[string]any{"q": "safe", "limit": float64(5)})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.(foldTwinInput); got != (foldTwinInput{Q: "safe", Limit: 5}) {
		t.Fatalf("got %+v", got)
	}
	// The tag-half/body-half route of refundInput still binds every half.
	out, err = ToolAdapter(Handler[refundInput, refundInput](echoRefund))(context.Background(),
		map[string]any{"id": "42", "notify": true, "amount": "9.50"})
	if err != nil || out.(refundInput) != (refundInput{ID: "42", Notify: true, Amount: "9.50"}) {
		t.Fatalf("got %+v %v", out, err)
	}
}
