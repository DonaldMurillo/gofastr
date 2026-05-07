package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/gofastr/gofastr/core/static"
	"github.com/gofastr/gofastr/framework"
)

func main() {
	app := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "static-site"}),
	)

	// Serve everything from the pages/ folder.
	// - /           → pages/index.html
	// - /about.html → pages/about.html
	// - /contact.html → pages/contact.html
	// - /style.css  → pages/style.css
	pagesDir := resolvePagesDir()

	static.Mount(app.Router, static.Config{
		FS:     os.DirFS(pagesDir),
		Prefix: "",
		// No SPA mode — only serve files that actually exist.
		// index.html is served automatically for "/".
	})

	log.Println("Static site example starting on :3070")
	log.Println("Serving files from:", pagesDir)
	log.Println("Open http://localhost:3070 in your browser")
	if err := app.Start(":3070"); err != nil {
		log.Fatal(err)
	}
}

// resolvePagesDir finds the pages/ directory relative to the working
// directory or the binary location, whichever exists.
func resolvePagesDir() string {
	candidates := []string{
		"pages",
		filepath.Join("examples", "static-site", "pages"),
	}
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			abs, _ := filepath.Abs(dir)
			return abs
		}
	}
	// Fallback: relative to this source file (won't work for go run,
	// but provides a clear error).
	log.Fatal("Could not find pages/ directory. Run from examples/static-site/")
	return ""
}
