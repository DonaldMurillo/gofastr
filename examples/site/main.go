// =============================================================================
// examples/site — the public GoFastr product site. Distinct from
// examples/website (which stays as the feature gallery / contributor demo).
//
// Boot: core-ui app + typed v2 theme + StyleSheet DSL output + UIHost on :8083.
// Dev livereload + SSE wiring all come for free via framework.NewApp.
// =============================================================================

package main

import (
	"fmt"
	"os"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

func main() {
	site := app.NewApp("GoFastr")

	t := createTheme()
	site.WithTheme(t)

	layout := app.NewLayout("main").
		WithHeader(&HeaderComponent{}).
		WithFooter(&FooterComponent{})
	site.SetDefaultLayout(layout)

	site.Register("/", &HomeScreen{}, nil)
	site.Register("/get-started", &GetStartedScreen{}, nil)
	site.Register("/docs/", &ConceptsIndexScreen{}, nil)
	// /docs/{slug} — single canonical doc page for now; the slug isn't used
	// (the screen renders the "entities" doc as the template). Wire dynamic
	// slugs once we route content out of framework/docs/content/*.md.
	site.Register("/docs/entities", &ConceptsDocScreen{}, nil)
	site.Register("/examples", &ExamplesScreen{}, nil)
	site.Register("/kiln", &KilnScreen{}, nil)
	site.Register("/philosophy", &PhilosophyScreen{}, nil)

	host := uihost.New(site,
		// All CSS for the site is built through the typed StyleSheet DSL
		// in styles.go — token-resolved from the theme, no raw strings.
		uihost.WithCustomCSS(createStyleSheet(t)),

		// Favicon: leaves the framework's /favicon.ico → 204 fallback in
		// place. When we have a real mark, set WithFavicon("/static/...")
		// here and drop the file under examples/site/static/.
		uihost.WithDescription("A pre-alpha Go full-stack framework where AI agents are first-class authors."),
		uihost.WithOpenGraph(uihost.OG{
			Title: "GoFastr",
			URL:   "https://gofastr.dev",
			Type:  "website",
		}),
		uihost.WithCanonicalURL("https://gofastr.dev"),
	)

	fwApp := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "site"}),
	)
	fwApp.Mount(host)

	addr := ":8083"
	fmt.Println("━─────────────────────────────────────────────")
	fmt.Println("  GoFastr — product site (v2)")
	fmt.Println("  http://localhost" + addr)
	fmt.Println("━─────────────────────────────────────────────")
	if err := fwApp.Start(addr); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}
