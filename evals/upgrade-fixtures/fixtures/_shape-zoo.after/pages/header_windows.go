//go:build windows

package pages

import "github.com/DonaldMurillo/gofastr/core/render"

func header() render.HTML { return render.Raw("<header>Zoo</header>") }
