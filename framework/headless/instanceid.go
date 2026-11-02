package headless

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// instanceID is the id a form control builds its own ids from (its
// input's id, its option ids, its hint and error ids). An explicit id
// is the caller's and is reserved as is, the way Carousel and Tabs
// reserve theirs. A derived id (the field Name, when no ID is given)
// goes through the render context's id scope (render.UniqueID), so a
// second control with the same Name on the page (a filter form and an
// edit form both naming a field "tags") gets "-2" instead of repeating
// every id and pointing each label's for= at the first one. With no
// scope on ctx, or the first instance on the page, the id is unchanged.
// family lists the suffixes the component builds from the id, taken
// with it so a sibling cannot be handed one of them.
func instanceID(ctx context.Context, explicit, derived string, family ...string) string {
	if explicit != "" {
		render.ReserveID(ctx, explicit, family...)
		return explicit
	}
	return render.UniqueID(ctx, derived, family...)
}
