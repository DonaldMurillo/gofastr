package schema

import (
	"context"
	"errors"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// Violation is one field-level validation failure: the phrase that
// names it and the values its message interpolates. It is an error
// whose Error() is the English message; [Violation.Text] renders it in
// the caller's locale, and [Violation.Code] is the stable code an API
// response carries next to the message.
//
// An app's own validators define their phrases the same way the
// built-in ones below are defined, so their messages translate too:
//
//	var vatInvalid = i18n.Define("validation.vat_invalid", "is not a valid VAT number")
//	... schema.Violation{Reason: vatInvalid}
type Violation struct {
	Reason i18n.Phrase
	Params map[string]any
}

// Error is the English message.
func (v Violation) Error() string { return v.Reason.Format(v.Params) }

// Text is the message in ctx's locale.
func (v Violation) Text(ctx context.Context) string { return v.Reason.Text(ctx, v.Params) }

// Code is the stable code: "required", "max_length", ...
func (v Violation) Code() string { return v.Reason.Code() }

// Phrased implements i18n.Phrased.
func (v Violation) Phrased() (i18n.Phrase, map[string]any) { return v.Reason, v.Params }

func violate(p i18n.Phrase, params map[string]any) error {
	return Violation{Reason: p, Params: params}
}

// AsViolation returns err as a Violation. An error that is not one
// (a validator that returned a plain error) becomes [ReasonInvalid]
// with the error's text as its detail.
func AsViolation(err error) Violation {
	if v, ok := errors.AsType[Violation](err); ok {
		return v
	}
	return Violation{Reason: ReasonInvalid, Params: map[string]any{"detail": err.Error()}}
}

// The built-in validation phrases, under "validation.<code>".
var (
	ReasonRequired         = i18n.Define("validation.required", "is required")
	ReasonInvalid          = i18n.Define("validation.invalid", "is invalid: {{detail}}")
	ReasonUnsupportedType  = i18n.Define("validation.unsupported_type", "has unsupported field type {{type}}")
	ReasonNotString        = i18n.Define("validation.not_string", "must be a string")
	ReasonMinLength        = i18n.Define("validation.min_length", "must be at least {{min}} characters")
	ReasonMaxLength        = i18n.Define("validation.max_length", "must be at most {{max}} characters")
	ReasonPatternInvalid   = i18n.Define("validation.pattern_invalid", "has an invalid pattern: {{detail}}")
	ReasonPattern          = i18n.Define("validation.pattern", "must match pattern {{pattern}}")
	ReasonNotInteger       = i18n.Define("validation.not_integer", "must be an integer")
	ReasonMin              = i18n.Define("validation.min", "must be at least {{min}}")
	ReasonMax              = i18n.Define("validation.max", "must be at most {{max}}")
	ReasonNotNumber        = i18n.Define("validation.not_number", "must be a number")
	ReasonNotFinite        = i18n.Define("validation.not_finite", "must be a finite number")
	ReasonNotDecimalString = i18n.Define("validation.not_decimal_string", "must be a decimal string")
	ReasonNotDecimal       = i18n.Define("validation.not_decimal", "must be a valid decimal number")
	ReasonNotFiniteDecimal = i18n.Define("validation.not_finite_decimal", "must be a finite decimal number")
	// ReasonDecimalExponent, ReasonDecimalIntegerDigits and
	// ReasonDecimalFractionDigits refuse a decimal its column cannot
	// store as sent (see [DecimalColumn]).
	ReasonDecimalExponent       = i18n.Define("validation.decimal_exponent", "must have an exponent between -{{max}} and {{max}}")
	ReasonDecimalIntegerDigits  = i18n.Define("validation.decimal_integer_digits", "must have at most {{max}} digits before the decimal point")
	ReasonDecimalFractionDigits = i18n.Define("validation.decimal_fraction_digits", "must have at most {{max}} digits after the decimal point")
	ReasonNotTrueFalse          = i18n.Define("validation.not_true_false", "must be true or false")
	ReasonNotBoolean            = i18n.Define("validation.not_boolean", "must be a boolean")
	ReasonNotOneOf              = i18n.Define("validation.not_one_of", "must be one of {{values}}")
	ReasonNotUUID               = i18n.Define("validation.not_uuid", "must be a valid UUID")
	ReasonNotTimestamp          = i18n.Define("validation.not_timestamp", "must be a valid RFC 3339 timestamp")
	ReasonNotDate               = i18n.Define("validation.not_date", "must be a valid date (YYYY-MM-DD)")
	ReasonNotJSON               = i18n.Define("validation.not_json", "must be valid JSON")
	ReasonNotReference          = i18n.Define("validation.not_reference", "must be a string reference")
	ReasonEmptyReference        = i18n.Define("validation.empty_reference", "must be a non-empty reference")
	ReasonNotPath               = i18n.Define("validation.not_path", "must be a string path or URL")
	ReasonEmptyPath             = i18n.Define("validation.empty_path", "must be a non-empty path or URL")
	ReasonUniqueTaken           = i18n.Define("validation.unique_taken", "is already in use")
)
