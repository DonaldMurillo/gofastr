// Package e pins the 2026-11 closure fix: an fs.FS handed to a
// function that RETURNS a handler — the http.HandlerFunc factory
// shape — was invisible to the fs.FS arm, which saw only the
// declaring function's own parameters. Each func literal is judged
// with its own parameters too, the way rootwrite already did.
package e

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

// handlerFor is the shape: the FS arrives on the factory, the request
// arrives on the closure it returns.
func handlerFor(assets fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := assets.Open(r.URL.Path) // want `read under a root with lexical containment only`
		if err != nil {
			http.NotFound(w, r)
			return
		}
		f.Close()
	}
}

// readerFor joins the factory's root with the closure's own name
// parameter: the closure's parameters are caller data for the reads in
// its body.
func readerFor(baseDir string) func(string) ([]byte, error) {
	return func(name string) ([]byte, error) {
		return os.ReadFile(filepath.Join(baseDir, name)) // want `read under a root with lexical containment only`
	}
}

// handlerLiteral reads a fixed name through the factory's fs: nothing
// caller-controlled. Quiet.
func handlerLiteral(assets fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(assets, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	}
}

// nonHTTPClosure has no request anywhere on the chain: a library
// factory, not a serving surface. Quiet.
func nonHTTPClosure(assets fs.FS) func(string) error {
	return func(name string) error {
		f, err := assets.Open(name)
		if err != nil {
			return err
		}
		return f.Close()
	}
}
