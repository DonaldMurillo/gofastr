package sitesearch

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// robotsScreen opts itself out through ScreenRobots, the per-screen
// directive uihost marks as the page's own.
type robotsScreen struct{ htmlScreen }

func (robotsScreen) ScreenRobots() string { return "noindex" }

// TestSitewideNoindexKeepsPages mounts the site behind a sitewide
// WithRobotsMeta("noindex"), the staging-deploy setting. That tag keeps
// crawlers off the deploy; it says nothing about the site's own search,
// which used to come up empty. A screen's own noindex still drops it.
func TestSitewideNoindexKeepsPages(t *testing.T) {
	site := coreapp.NewApp("sitesearch-robots")
	site.Register("/", htmlScreen(`<main><h1>Home</h1><p>Shared validation guide.</p></main>`), nil)
	site.Register("/docs/forms", htmlScreen(`<main><h1>Forms</h1><p>Validation renders beside each field.</p></main>`), nil)
	site.Register("/drafts", robotsScreen{htmlScreen(`<main><h1>Draft validation notes</h1></main>`)}, nil)
	app := framework.NewApp(framework.WithoutDefaultMiddleware()).
		Mount(uihost.New(site, uihost.WithRobotsMeta("noindex, nofollow")))
	if err := app.Batteries.Register(New(Config{})); err != nil {
		t.Fatal(err)
	}
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	rec := get(t, app.Router(), Root+"query?q=validation")
	if rec.Code != http.StatusOK {
		t.Fatalf("query = %d %s", rec.Code, rec.Body)
	}
	var body struct{ Results []Result }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range body.Results {
		got = append(got, r.URL)
	}
	slices.Sort(got)
	if want := []string{"/", "/docs/forms"}; !slices.Equal(got, want) {
		t.Fatalf("indexed %v, want %v (/drafts opts out itself)", got, want)
	}
}
