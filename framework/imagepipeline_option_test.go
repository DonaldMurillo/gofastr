package framework

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/file"
)

// stubDeriver is an identity ImageDeriver: the options under test only store
// and route it, so the tests assert WHICH deriver a field resolves to, not
// what deriving does.
type stubDeriver struct{ id string }

func (s stubDeriver) DeriveImage(ctx context.Context, store upload.Storage, data []byte, primaryRef string) (*file.ImageDerivatives, error) {
	return nil, nil
}

// WithImagePipeline sets the app-wide deriver; WithImagePipelineFor overrides
// it per (entity, field), because one config cannot describe both an avatar
// and a hero cover. A nil per-field deriver opts that field out entirely
// without disturbing the app-wide default, so "absent" and "explicitly none"
// have to stay distinguishable.
func TestWithImagePipeline_AppWideAndPerFieldOverride(t *testing.T) {
	appWide := stubDeriver{id: "app-wide"}
	avatars := stubDeriver{id: "avatars"}

	app := NewApp(
		WithoutDefaultMiddleware(),
		WithImagePipeline(appWide),
		WithImagePipelineFor("users", "avatar", avatars),
		WithImagePipelineFor("users", "banner", nil), // explicit opt-out
	)

	if app.imageDeriver == nil {
		t.Fatal("WithImagePipeline did not set the app-wide deriver")
	}
	if got := app.imageDeriver.(stubDeriver).id; got != "app-wide" {
		t.Errorf("app-wide deriver = %q, want %q", got, "app-wide")
	}

	fields, ok := app.fieldImageDerivers["users"]
	if !ok {
		t.Fatal("WithImagePipelineFor did not register the users entity")
	}

	got, ok := fields["avatar"]
	if !ok {
		t.Fatal("avatar override missing")
	}
	if got.(stubDeriver).id != "avatars" {
		t.Errorf("avatar deriver = %q, want %q", got.(stubDeriver).id, "avatars")
	}

	// The opt-out must be PRESENT and nil, an absent key would fall back to
	// the app-wide deriver, which is the opposite of opting out.
	banner, present := fields["banner"]
	if !present {
		t.Error("nil override was dropped; the field would fall back to the app-wide deriver")
	}
	if banner != nil {
		t.Errorf("banner deriver = %v, want nil", banner)
	}

	// A field with no entry at all is untouched.
	if _, present := fields["nonexistent"]; present {
		t.Error("an unregistered field somehow has an entry")
	}
}

// The per-field map is built lazily, so registering an override without any
// app-wide default must not panic on a nil map.
func TestWithImagePipelineFor_WithoutAppWideDefault(t *testing.T) {
	app := NewApp(
		WithoutDefaultMiddleware(),
		WithImagePipelineFor("posts", "cover", stubDeriver{id: "covers"}),
	)
	if app.imageDeriver != nil {
		t.Error("no app-wide deriver was configured, but one is set")
	}
	if got := app.fieldImageDerivers["posts"]["cover"].(stubDeriver).id; got != "covers" {
		t.Errorf("cover deriver = %q, want %q", got, "covers")
	}
}
