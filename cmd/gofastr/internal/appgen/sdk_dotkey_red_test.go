//go:build red

package appgen

import "testing"

// The generated record methods build the path with url.PathEscape per key
// column, which leaves "." and ".." as they are. The server reads them as
// dot segments (404 for "..", a redirect to the list for "."), so a row
// whose key is "." or ".." can be created through the typed repository and
// is served by the REST API at /tags/%2E%2E, but Get, Update, Patch and
// Delete on the Local and the Remote repository can never reach it.
//
// The fixture below runs inside the generated module, as TestSDKRepoParity's
// does, against one app over both adapters.
const bughuntDotKeyFixture = `package client_test

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"testing"

	client "example.com/repo"
	"example.com/repo/local"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func TestDotKeysRoundTrip(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if _, err := database.Exec("CREATE TABLE tags (slug TEXT PRIMARY KEY, label TEXT)"); err != nil {
		t.Fatal(err)
	}
	public := true
	application := framework.NewApp(framework.WithDB(database), framework.WithAPIPrefix("api"))
	application.Entity("tags", framework.EntityConfig{
		PrimaryKey: []string{"slug"},
		Fields:     []schema.Field{{Name: "slug", Type: schema.String, Required: true}, {Name: "label", Type: schema.String}},
		Exposure:   &framework.ExposureConfig{CRUD: &public, Public: true},
	}.WithTimestamps(false))
	server := httptest.NewServer(application.Router())
	defer server.Close()
	api := client.NewClient(server.URL+"/api", nil)
	for name, repo := range map[string]client.TagsRepo{
		"Local":  local.NewTagsLocal(application.InProcessHandler()),
		"Remote": client.NewTagsRemote(api),
	} {
		// Control: keys holding every other path-shaping byte round-trip.
		for _, slug := range []string{"a/b", "a b", "a%2Fb", "x?y", "h#i", "a/../b", name + "-plain"} {
			if _, err := repo.Create(context.Background(), client.TagsInput{Slug: slug}); err != nil {
				t.Fatalf("%s control create %q: %v", name, slug, err)
			}
			if got, err := repo.Get(context.Background(), slug); err != nil || got.Slug != slug {
				t.Fatalf("%s control get %q: %+v, %v", name, slug, got, err)
			}
			if err := repo.Delete(context.Background(), slug); err != nil {
				t.Fatalf("%s control delete %q: %v", name, slug, err)
			}
		}
		for _, slug := range []string{"..", "."} {
			created, err := repo.Create(context.Background(), client.TagsInput{Slug: slug})
			if err != nil || created.Slug != slug {
				t.Fatalf("%s create %q: %+v, %v", name, slug, created, err)
			}
			// The REST API serves the row at its escaped path.
			var raw struct {
				Data client.Tags ` + "`json:\"data\"`" + `
			}
			escaped := map[string]string{"..": "%2E%2E", ".": "%2E"}[slug]
			if err := api.Do(context.Background(), "GET", "/tags/"+escaped, nil, &raw); err != nil || raw.Data.Slug != slug {
				t.Fatalf("REST GET /tags/%s: %+v, %v", escaped, raw, err)
			}
			if got, err := repo.Get(context.Background(), slug); err != nil || got.Slug != slug {
				t.Errorf("%s: Get(%q) after Create(%q) = %+v, %v; want the created row", name, slug, slug, got, err)
			}
			if err := repo.Delete(context.Background(), slug); err != nil {
				t.Errorf("%s: Delete(%q) = %v; want nil", name, slug, err)
				_ = api.Do(context.Background(), "DELETE", "/tags/"+escaped, nil, nil)
			}
		}
	}
}
`

// Open finding from the 2026-11-10 bug hunt, kept as a red probe: it
// asserts the correct behaviour and fails until the fix lands. A typed
// repository or generated client cannot address a row keyed "." or "..":
// url.PathEscape leaves a dot segment bare and the router resolves it
// away. The generators cannot fix it alone: entity.JoinKey and SplitKey
// own the key spelling (SplitKey refuses a part that is not exactly its
// PathEscape), so the rule has to change there first, then in the Go
// client, the JS SDK and both generated CLIs together.
func TestRedSDKDotKeys(t *testing.T) {
	if output, err := bughuntRunSDKFixture(t, bughuntDotKeyFixture, "TestDotKeysRoundTrip"); err != nil {
		t.Fatalf("generated SDK cannot address a row keyed \".\" or \"..\": %v\n%s", err, output)
	}
}
