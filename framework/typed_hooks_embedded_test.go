package framework

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
)

type embedSecretFields struct {
	Secret string `json:"secret"`
}

type embedRow struct {
	embedSecretFields
	Title string `json:"title"`
}

// A hook's write to a field promoted from an embedded struct lands on
// that field's own key, as JSON promotes it, never on a key named
// after the embedded type.
func TestTypedHookEmbeddedFieldMerges(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	OnBeforeCreate[embedRow](app, "rows", func(_ context.Context, v *embedRow) error {
		v.Secret = ""
		return nil
	})
	body := map[string]any{"title": "t", "secret": "s3cr3t"}
	if err := app.HookRegistry("rows").ExecuteHooks(context.Background(), hook.BeforeCreate, body); err != nil {
		t.Fatal(err)
	}
	if body["secret"] != "" {
		t.Errorf("secret = %v, want the hook's empty value", body["secret"])
	}
	for k := range body {
		if k != "title" && k != "secret" {
			t.Errorf("merge added key %q", k)
		}
	}
}
