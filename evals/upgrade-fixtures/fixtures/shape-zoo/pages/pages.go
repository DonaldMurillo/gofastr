package pages

import "github.com/DonaldMurillo/gofastr/core/render"

// All renders every shape so nothing is dead code.
func All() []render.HTML {
	out := []render.HTML{saveButton(), labelledButton(), header(), footer(), table()}
	out = append(out, forms()...)
	for _, s := range markup {
		out = append(out, render.Raw(s))
	}
	return out
}
