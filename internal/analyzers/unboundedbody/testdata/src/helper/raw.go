package helper

import (
	"io"
	"net/http"
)

// uncapped never calls a capping helper, so it is still reported even
// though the package has one. The helper allowance widens what counts as
// a cap; it must not switch the check off package-wide.
func uncapped(r *http.Request) {
	_, _ = io.ReadAll(r.Body) // want "reads an inbound request body with no size cap"
}
