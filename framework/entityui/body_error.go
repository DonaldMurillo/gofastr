package entityui

import (
	"io"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/internal/bodylimit"
)

// decodeBody caps r.Body at limit and decodes it with
// handler.UnmarshalStrict, answering through writeBulkError: 413 over
// the cap, 400 naming an unreadable body, 400 naming invalid JSON. It
// reports whether the caller may read on.
func decodeBody(w http.ResponseWriter, r *http.Request, limit int64, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		status, phrase := bodylimit.ReadRefusal(err)
		writeBulkError(w, r, status, phrase)
		return false
	}
	if err := handler.UnmarshalStrict(data, dst); err != nil {
		status, phrase := bodylimit.DecodeRefusal(err)
		writeBulkError(w, r, status, phrase)
		return false
	}
	return true
}

// parseBodyForm caps r.Body at limit and parses its form, answering
// through writeBulkError: 413 over the cap, else 400 naming invalid
// form data. It reports whether the caller may read on.
func parseBodyForm(w http.ResponseWriter, r *http.Request, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := r.ParseForm(); err != nil {
		status, phrase := bodylimit.FormRefusal(err)
		writeBulkError(w, r, status, phrase)
		return false
	}
	return true
}
