// Package handler is the stub of core/handler the rawtext fixtures call.
package handler

import "github.com/DonaldMurillo/gofastr/core/i18n"

type Error struct {
	Code    int
	Reason  i18n.Phrase
	Message string
}

func (e *Error) Error() string { return e.Message }

func Errorf(code int, format string, args ...any) *Error {
	return &Error{Code: code, Message: format} // want `handler.Error.Message sends text as written`
}

func Fail(code int, reason i18n.Phrase) *Error { return &Error{Code: code, Reason: reason} }
