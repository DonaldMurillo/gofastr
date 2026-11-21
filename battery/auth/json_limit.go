package auth

import (
	"errors"
	"io"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// maxAuthBodyBytes caps every JSON body decoded by an auth handler. It
// equals internal/bodylimit.MaxJSONBodyBytes; auth keeps its own copy so
// it stays unmountable (TestBodyRefusalsMatchBodylimit).
const maxAuthBodyBytes int64 = 1 << 20 // 1 MiB

func decodeJSONLimited(w http.ResponseWriter, r *http.Request, dst any) bool {
	if !handler.IsJSONContentType(r.Header.Get("Content-Type")) {
		writeAuthError(w, r, http.StatusUnsupportedMediaType, i18n.ReasonJSONMediaType)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeAuthError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
			return false
		}
		writeAuthError(w, r, http.StatusBadRequest, i18n.ReasonBodyUnreadable)
		return false
	}
	if err := handler.UnmarshalStrict(body, dst); err != nil {
		writeAuthError(w, r, http.StatusBadRequest, i18n.ReasonInvalidJSONBody)
		return false
	}
	return true
}
