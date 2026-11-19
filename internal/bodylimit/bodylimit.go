// Package bodylimit owns the repo's answers for an HTTP request body
// refused before a handler runs it: the cap on an inbound JSON body, and
// the status and phrase for a body that passed its cap, could not be
// read, or would not decode. Each surface keeps its own envelope writer;
// the status and phrase decision is made here.
package bodylimit

import (
	"errors"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// MaxJSONBodyBytes is the repo's cap on one inbound JSON request body
// (a crud write, an auth credential, an admin ops post, a semantic
// query, a desktop bridge call); a larger body answers 413. A cap that
// belongs to one surface (a picker's 4 KiB, an embed exchange's 8 KiB,
// multipart's 64 MiB) stays with that surface.
const MaxJSONBodyBytes int64 = 1 << 20 // 1 MiB

// TooLarge reports whether err is a body that passed the cap its reader
// enforced, however the overflow is wrapped.
func TooLarge(err error) bool {
	_, ok := errors.AsType[*http.MaxBytesError](err)
	return ok
}

// TooLargeRefusal is the status and phrase every over-cap body answers.
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
