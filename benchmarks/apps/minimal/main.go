// minimal is the smallest meaningful GoFastr binary: NewApp + one
// plaintext route. Establishes the floor for binary size and idle RAM.
//
// No DB, no entities, no UI. Listens on $PORT (default :18080).
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gofastr/gofastr/framework"
)

func main() {
	app := framework.NewApp()
	app.Router.GetFunc("/plaintext", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("Hello, World!"))
	})
	addr := ":" + getEnv("PORT", "18080")
	log.Printf("minimal listening on %s", addr)
	if err := app.Start(addr); err != nil {
		log.Fatal(err)
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
