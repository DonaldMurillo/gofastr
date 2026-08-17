package framework

import (
	"bytes"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// The startup banner must not advertise a route an anonymous curl cannot
// reach without saying so: a non-Public entity and a non-public API surface
// answer 401, and a fresh `gofastr init` user's first action is to curl the
// banner's URLs.
func TestBannerMarksAuthGatedRoutes(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.Entity("posts", EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	app.Entity("pages", EntityConfig{
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
		Exposure: &ExposureConfig{Public: true},
	}.WithTimestamps(false))
	var out bytes.Buffer
	app.startupOutput = &out

	app.printStartupBanner("127.0.0.1:8080", "test", true, true, "")
	got := out.String()

	for _, line := range strings.Split(got, "\n") {
		switch {
		case strings.Contains(line, "/posts"):
			if !strings.Contains(line, "requires auth") {
				t.Errorf("non-public entity line lacks the auth marker: %q", line)
			}
		case strings.Contains(line, "/pages"):
			if strings.Contains(line, "requires auth") {
				t.Errorf("public entity line wrongly marked auth-gated: %q", line)
			}
		case strings.Contains(line, "/openapi.json"), strings.Contains(line, "/api/docs/"), strings.Contains(line, "/api/llm.md"):
			if !strings.Contains(line, "requires auth") {
				t.Errorf("gated API line lacks the auth marker: %q", line)
			}
		}
	}
}

func TestBannerPublicOpenAPIUnmarked(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware(), WithPublicOpenAPI())
	var out bytes.Buffer
	app.startupOutput = &out

	app.printStartupBanner("127.0.0.1:8080", "test", true, true, "")

	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, "/openapi.json") || strings.Contains(line, "/api/docs/") || strings.Contains(line, "/api/llm.md") {
			if strings.Contains(line, "requires auth") {
				t.Errorf("public API surface wrongly marked auth-gated: %q", line)
			}
		}
	}
}

// TestStartupBannerVersionedEntityPathAndLabel: an entity mounted under a
// versioned route group (App.GroupEntity) must advertise its version-prefixed
// mount path and carry the "(version)" label, not the bare "/table" path a
// single-version entity gets. A user curling the banner URL for /v2/widgets
// must reach it; printing "/widgets" would advertise a 404.
func TestStartupBannerVersionedEntityPathAndLabel(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	g := app.Group("/v2")
	app.GroupEntity(g, "widgets", EntityConfig{
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
		Exposure: &ExposureConfig{CRUD: boolPtr(false)},
	}.WithTimestamps(false))
	var out bytes.Buffer
	app.startupOutput = &out

	app.printStartupBanner("127.0.0.1:8080", "test", false, false, "")
	got := out.String()

	if !strings.Contains(got, "widgets (/v2)") {
		t.Errorf("versioned entity label missing; banner:\n%s", got)
	}
	if !strings.Contains(got, "127.0.0.1:8080/v2/widgets") {
		t.Errorf("versioned mount path missing; banner:\n%s", got)
	}
}
