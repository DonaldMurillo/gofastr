package framework

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// hookSnakeAccount tags its fields in snake_case, the spelling most
// hand-written Go structs use. Generated models tag in camelCase.
type hookSnakeAccount struct {
	Title   string `json:"title"`
	IsAdmin bool   `json:"is_admin"`
	APIKey  string `json:"api_key"`
}

// TestTypedHookSeesSnakeTaggedFields: a typed hook decoded its payload
// with only camelCase keys, so a T tagged `json:"is_admin"` read the zero
// value. A Before hook forcing IsAdmin=false then compared equal to the
// zero it started from and the client's is_admin=true was written; an
// After hook clearing APIKey changed nothing, and the key went out in the
// response. Every typed write surface, on both payload casings, sees and
// writes back a snake-tagged field.
func TestTypedHookSeesSnakeTaggedFields(t *testing.T) {
	type surface struct {
		name    string
		phase   hook.HookType
		body    map[string]any
		key     string // the payload's own key for the field under test
		want    any
		install func(app *App, saw *bool)
	}
	demote := func(_ context.Context, v *hookSnakeAccount, saw *bool) error {
		*saw = v.IsAdmin
		v.IsAdmin = false
		return nil
	}
	redact := func(_ context.Context, v *hookSnakeAccount, saw *bool) error {
		*saw = v.APIKey != ""
		v.APIKey = ""
		return nil
	}
	surfaces := []surface{
		{"before-create", hook.BeforeCreate, map[string]any{"title": "t", "is_admin": true}, "is_admin", false,
			func(app *App, saw *bool) {
				OnBeforeCreate[hookSnakeAccount](app, "accts", func(c context.Context, v *hookSnakeAccount) error { return demote(c, v, saw) })
			}},
		{"before-update", hook.BeforeUpdate, map[string]any{"is_admin": true}, "is_admin", false,
			func(app *App, saw *bool) {
				OnBeforeUpdate[hookSnakeAccount](app, "accts", func(c context.Context, v *hookSnakeAccount) error { return demote(c, v, saw) })
			}},
		// After payloads are the response map, camelCase by default.
		{"after-create", hook.AfterCreate, map[string]any{"title": "t", "apiKey": "sk_live_1"}, "apiKey", "",
			func(app *App, saw *bool) {
				OnAfterCreate[hookSnakeAccount](app, "accts", func(c context.Context, v *hookSnakeAccount) error { return redact(c, v, saw) })
			}},
		{"after-update", hook.AfterUpdate, map[string]any{"title": "t", "apiKey": "sk_live_1"}, "apiKey", "",
			func(app *App, saw *bool) {
				OnAfterUpdate[hookSnakeAccount](app, "accts", func(c context.Context, v *hookSnakeAccount) error { return redact(c, v, saw) })
			}},
	}
	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			app := NewApp(WithoutDefaultMiddleware())
			var saw bool
			n := len(s.body)
			s.install(app, &saw)
			if err := app.HookRegistry("accts").ExecuteHooks(context.Background(), s.phase, s.body); err != nil {
				t.Fatal(err)
			}
			if !saw {
				t.Errorf("the hook read the zero value for a snake-tagged field the payload carries: %v", s.body)
			}
			if s.body[s.key] != s.want {
				t.Errorf("SECURITY: payload[%s] = %v after the hook set it to %v: %v", s.key, s.body[s.key], s.want, s.body)
			}
			if len(s.body) != n {
				t.Errorf("payload widened with alias keys: %v", s.body)
			}
		})
	}
}
