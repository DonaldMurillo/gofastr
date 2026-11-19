package entityui

import (
	"errors"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// writeBodyError answers a failed request-body decode: 413 when the body
// passed its MaxBytesReader limit, else 400 with the invalid-JSON reason.
func writeBodyError(w http.ResponseWriter, r *http.Request, err error) {
	if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
		writeBulkError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
		return
	}
	writeBulkError(w, r, http.StatusBadRequest, i18n.ReasonInvalidJSONBody)
}
