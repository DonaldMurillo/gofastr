// Package bodylimit defines the inbound JSON body cap and the standard refusal status and phrase.
// Oversize admin form posts redirect with 303 and result=body-too-large. Form RPCs return 413 with the localized admin body-too-large message.
package bodylimit

import (
	"errors"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// MaxJSONBodyBytes is the cap for inbound JSON bodies, including CRUD, auth, admin ops,
// semantic queries and the desktop bridge. Surface-specific caps stay with their surfaces.
const MaxJSONBodyBytes int64 = 1 << 20 // 1 MiB

// TooLarge reports whether err is a body that passed the cap its reader
// enforced, however the overflow is wrapped.
func TooLarge(err error) bool {
	_, ok := errors.AsType[*http.MaxBytesError](err)
	return ok
}

// TooLargeRefusal returns the standard status and phrase for an over-cap body.
func TooLargeRefusal() (int, i18n.Phrase) {
	return http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge)
}

// ReadRefusal is the status and phrase for a body whose read failed
// before any decode ran: 413 when it passed its cap, else 400 naming a
// body that could not be read.
func ReadRefusal(err error) (int, i18n.Phrase) {
	if TooLarge(err) {
		return TooLargeRefusal()
	}
	return http.StatusBadRequest, i18n.ReasonBodyUnreadable
}

// FormRefusal is ReadRefusal for a parsed form body: 413 when it passed
// its cap, else 400 naming invalid form data.
func FormRefusal(err error) (int, i18n.Phrase) {
	if TooLarge(err) {
		return TooLargeRefusal()
	}
	return http.StatusBadRequest, i18n.ReasonInvalidForm
}

// DecodeRefusal is the status and phrase for a body that reached the
// decoder and was refused: 413 when it passed its cap, else 400 naming
// invalid JSON.
func DecodeRefusal(err error) (int, i18n.Phrase) {
	if TooLarge(err) {
		return TooLargeRefusal()
	}
	return http.StatusBadRequest, i18n.ReasonInvalidJSONBody
}
