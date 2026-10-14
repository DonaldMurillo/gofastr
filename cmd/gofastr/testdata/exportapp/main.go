// Command exportapp is the fixture `gofastr export` runs in its test: a
// two-page site with the search battery registered.
package main

import (
	"log"

	"github.com/DonaldMurillo/gofastr/battery/sitesearch"
	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type page string

func (p page) Render() render.HTML { return render.HTML(p) }

func main() {
	site := coreapp.NewApp("exportapp")
	site.Register("/", page(`<main><h1>Home</h1><p>Welcome.</p></main>`), nil)
	site.Register("/about", page(`<main><h1>About</h1><p>Wombats live here.</p></main>`), nil)
	app := framework.NewApp().Mount(uihost.New(site))
	if err := app.Batteries.Register(sitesearch.New(sitesearch.Config{})); err != nil {
		log.Fatal(err)
	}
	if err := app.Start("127.0.0.1:0"); err != nil {
		log.Fatal(err)
	}
}
