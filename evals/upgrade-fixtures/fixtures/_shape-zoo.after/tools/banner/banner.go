// Package banner lives in a nested module the root ./... never loads.
package banner

import "github.com/DonaldMurillo/gofastr/core/render"

func Banner() render.HTML { return render.Raw("<header>Zoo</header>") }
