package packcmd

import (
	"maps"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// An entity whose create is open to anyone (a contact form, an inquiry)
// is generated as Create: framework.Anyone, a selector, not a string
// literal. Pack read only literals, so the packed spec lost the grant and
// the app regenerated from it refused every anonymous submission.
func TestPack_AccessAnyoneRoundTrips(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Contact", Module: "example.com/contact", DBDriver: "sqlite", DBURL: "file:x.db"},
		Entities: []framework.EntityDeclaration{{
			Name: "contacts",
			Exposure: &framework.ExposureDeclaration{Access: &framework.AccessDeclaration{
				Read: "contacts:read", Create: framework.Anyone, Update: "contacts:write", Delete: "contacts:admin",
			}},
			Fields: []framework.FieldDeclaration{{Name: "email", Type: "string", Required: true}},
		}},
	}
	dir := materializeAppSpec(t, bp)
	got, err := packAppSpec(dir)
	if err != nil {
		t.Fatalf("packAppSpec: %v", err)
	}
	if len(got.Entities) != 1 || got.Entities[0].Exposure == nil || got.Entities[0].Exposure.Access == nil {
		t.Fatalf("access lost on read-back: %#v", got.Entities)
	}
	acc := got.Entities[0].Exposure.Access
	want := framework.AccessDeclaration{Read: "contacts:read", Create: framework.Anyone, Update: "contacts:write", Delete: "contacts:admin"}
	if *acc != want {
		t.Errorf("access = %+v, want %+v", *acc, want)
	}
}

// The generator writes app.theme.dark as an overlay on the framework's
// dark palette: theme.DarkColors = uitheme.Default().DarkColors, then one
// index assignment per key, plus a derived ink for a fill the spec names
// without its -fg. Pack read only the old map-literal spelling, so the
// whole dark palette (and the header's theme toggle, which hangs off it)
// vanished from the packed spec.
func TestPack_DarkThemeOverlayRoundTrips(t *testing.T) {
	dark := map[string]string{"background": "#15141B", "primary": "#8B80F2", "text": "#ECEAF3"}
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{
			Name: "Dark", Module: "example.com/dark", DBDriver: "sqlite", DBURL: "file:x.db",
			Theme:     map[string]string{"primary": "#4338CA"},
			ThemeDark: dark,
		},
		Entities: []framework.EntityDeclaration{{
			Name:   "posts",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string", Required: true}},
		}},
	}
	dir := materializeAppSpec(t, bp)
	got, err := packAppSpec(dir)
	if err != nil {
		t.Fatalf("packAppSpec: %v", err)
	}
	if !maps.Equal(got.App.ThemeDark, dark) {
		t.Errorf("theme.dark = %v, want %v (the derived primary-fg ink is not authored)", got.App.ThemeDark, dark)
	}
}
