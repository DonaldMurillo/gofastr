package main

import (
	"fmt"
	"net/http"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/signal"
)

func main() {
	// Create app
	application := app.NewApp("GoFastr Demo")

	// Set theme
	theme := createTheme()
	application.WithTheme(theme)

	// Create layout
	layout := app.NewLayout("main").
		WithHeader(&HeaderComponent{}).
		WithFooter(&FooterComponent{})

	application.SetDefaultLayout(layout)

	// Register screens
	cartCount := signal.New(0)
	application.RegisterScreen(app.NewScreen("/", &HomeScreen{}), nil)
	application.RegisterScreen(app.NewScreen("/products", &ProductListScreen{}), nil)
	application.RegisterScreen(app.NewScreen("/about", &AboutScreen{}), nil)
	application.RegisterScreen(app.NewDrawer("/cart", &CartDrawer{CartCount: cartCount}), nil)

	// Serve
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		html, err := application.RenderPage(r.URL.Path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	})

	fmt.Println("Demo running on :8080")
	http.ListenAndServe(":8080", nil)
}
