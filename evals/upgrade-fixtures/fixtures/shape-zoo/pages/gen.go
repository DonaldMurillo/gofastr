//go:build ignore

// gen.go is a `go run gen.go` generator: package main in another
// package's directory, built only when named on the command line.
package main

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func main() { fmt.Println(ui.SiteHeader(ui.SiteHeaderConfig{})) } // zoo:hit siteheader
