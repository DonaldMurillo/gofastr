package framework

import (
	"bytes"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// #503: with secure-by-default CRUD and no auth wired yet, every entity
// URL in the banner answers 401 and nothing says how to get past it. The
// banner names the fix once, and only until something notes that it
// signs callers in (battery/auth's Init calls NoteAuthenticator).
func TestBannerHintsWhenNoAuthIsMounted(t *testing.T) {
	const hint = "No auth is wired"
	posts := EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}}.WithTimestamps(false)

	render := func(app *App) string {
		var out bytes.Buffer
		app.startupOutput = &out
		app.printStartupBanner("127.0.0.1:8080", "test", false, false, "")
		return out.String()
	}

	bare := NewApp(WithoutDefaultMiddleware(), WithDB(bannerTestDB(t)))
	bare.Entity("posts", posts)
	if got := render(bare); strings.Count(got, hint) != 1 {
		t.Errorf("gated entity, no auth battery: want the hint once:\n%s", got)
	}

	authed := NewApp(WithoutDefaultMiddleware(), WithDB(bannerTestDB(t)))
	authed.Entity("posts", posts)
	authed.NoteAuthenticator()
	if got := render(authed); strings.Contains(got, hint) {
		t.Errorf("auth is noted: the hint must not print:\n%s", got)
	}

	// A battery merely NAMED auth does not sign anyone in.
	named := NewApp(WithoutDefaultMiddleware(), WithDB(bannerTestDB(t)))
	named.Entity("posts", posts)
	named.RegisterBattery(&mockBattery{name: "auth"})
	if got := render(named); !strings.Contains(got, hint) {
		t.Errorf("a battery that never notes auth is not auth: want the hint:\n%s", got)
	}

	public := NewApp(WithoutDefaultMiddleware(), WithDB(bannerTestDB(t)))
	public.Entity("pages", EntityConfig{
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
		Exposure: &ExposureConfig{Public: true},
	}.WithTimestamps(false))
	if got := render(public); strings.Contains(got, hint) {
		t.Errorf("only Public entities: nothing answers 401, the hint must not print:\n%s", got)
	}
}
