package framework

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
)

type hookCard struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	CardNumber string `json:"cardNumber"`
	Line2      string `json:"line2"`
}

// The After payload is the response map, keyed by wire (camelCase) names.
// A typed hook's redaction lands on that key, not on a new snake_case one
// beside the original value.
func TestTypedAfterHookRedactsCamelKey(t *testing.T) {
	for _, phase := range []hook.HookType{hook.AfterCreate, hook.AfterUpdate} {
		app := NewApp(WithoutDefaultMiddleware())
		redact := func(_ context.Context, v *hookCard) error { v.CardNumber = ""; return nil }
		if phase == hook.AfterCreate {
			OnAfterCreate[hookCard](app, "cards", redact)
		} else {
			OnAfterUpdate[hookCard](app, "cards", redact)
		}
		body := map[string]any{"id": "1", "title": "visa", "cardNumber": "4111111111111111"}
		if err := app.HookRegistry("cards").ExecuteHooks(context.Background(), phase, body); err != nil {
			t.Fatal(err)
		}
		if body["cardNumber"] != "" {
			t.Fatalf("%s: cardNumber = %v, want redacted", phase, body["cardNumber"])
		}
		if _, ok := body["card_number"]; ok {
			t.Fatalf("%s: stray snake key added: %v", phase, body)
		}
	}
}

// A column with a digit segment (line_2) camelCases to line2, which
// snake-cases back to line2. The Before hook's change lands on the
// payload's own key.
func TestTypedBeforeHookKeepsDigitKey(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	OnBeforeCreate[hookCard](app, "cards", func(_ context.Context, v *hookCard) error {
		v.Line2 = "Suite 4"
		return nil
	})
	body := map[string]any{"title": "visa", "line_2": "typed by caller"}
	if err := app.HookRegistry("cards").ExecuteHooks(context.Background(), hook.BeforeCreate, body); err != nil {
		t.Fatal(err)
	}
	if body["line_2"] != "Suite 4" {
		t.Fatalf("line_2 = %v, want the hook's value: %v", body["line_2"], body)
	}
}
