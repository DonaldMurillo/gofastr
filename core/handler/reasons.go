package handler

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The request-binding refusals, under "error.<code>". ReasonInvalidJSON's
// detail param is a JSONErrorDetail phrase, never the decoder's own text.
var (
	ReasonInvalidJSON          = i18n.Define("error.invalid_json", "invalid JSON: {{detail}}")
	ReasonJSONMultipleValues   = i18n.Define("error.json_multiple_values", "invalid JSON: body must contain exactly one JSON value")
	ReasonJSONDuplicateKey     = i18n.Define("error.json_duplicate_key", "invalid JSON: duplicate key {{key}}")
	ReasonJSONUnknownField     = i18n.Define("error.json_unknown_field", "invalid JSON: unknown field {{key}}")
	ReasonJSONFoldedKeys       = i18n.Define("error.json_folded_keys", "invalid JSON: keys {{first}} and {{second}} name the same field")
	ReasonInvalidParameter     = i18n.Define("error.invalid_parameter", "invalid {{source}} parameter {{name}}: {{detail}}")
	ReasonParameterControlByte = i18n.Define("error.parameter_control_byte", "invalid {{source}} parameter {{name}}: it carries a byte a {{source}} value never may")
	ReasonInvalidHeader        = i18n.Define("error.invalid_header", "invalid header {{name}}: {{detail}}")
	ReasonInvalidParams        = i18n.Define("error.invalid_params", "invalid params: {{detail}}")
)

// The duplicate and folded key refusals for an object below the top
// level, naming where it sits. The code (the last key segment) is the
// top-level one's, so a client branching on "reason" sees no difference.
var (
	reasonJSONDuplicateKeyAt = i18n.Define("error.nested.json_duplicate_key", "invalid JSON: duplicate key {{key}} in {{path}}")
	reasonJSONFoldedKeysAt   = i18n.Define("error.nested.json_folded_keys", "invalid JSON: keys {{first}} and {{second}} in {{path}} name the same field")
)

// The {{detail}} of a JSON decode refusal (JSONErrorDetail): what was
// wrong and where, in JSON's own terms. A path is the client's key
// spelling (items[2].price); a type is one of JSON's six.
var (
	jsonWrongType          = i18n.Define("json.decode.wrong_type", "{{path}}: expected {{expected}}, got {{got}}")
	jsonWrongTypeRoot      = i18n.Define("json.decode.wrong_type_root", "expected {{expected}}, got {{got}}")
	jsonUnexpectedType     = i18n.Define("json.decode.unexpected_type", "{{path}}: {{got}} is not accepted here")
	jsonUnexpectedTypeRoot = i18n.Define("json.decode.unexpected_type_root", "{{got}} is not accepted here")
	jsonIntegerRange       = i18n.Define("json.decode.integer_range", "{{path}}: expected a whole number in range")
	jsonIntegerRangeRoot   = i18n.Define("json.decode.integer_range_root", "expected a whole number in range")
	jsonNumberRange        = i18n.Define("json.decode.number_range", "{{path}}: number out of range")
	jsonNumberRangeRoot    = i18n.Define("json.decode.number_range_root", "a number is out of range")
	jsonSyntax             = i18n.Define("json.decode.syntax", "syntax error at byte {{offset}}")
	jsonUnexpectedEnd      = i18n.Define("json.decode.unexpected_end", "the body ends before the JSON value is complete")
	jsonEmpty              = i18n.Define("json.decode.empty", "the body is empty")
	jsonTooLarge           = i18n.Define("json.decode.too_large", "the body is too large")
	jsonUnreadable         = i18n.Define("json.decode.unreadable", "the body could not be read")
	jsonUnknownField       = i18n.Define("json.decode.unknown_field", "unknown field {{key}}")
	jsonTrailingData       = i18n.Define("json.decode.trailing_data", "unexpected data after the JSON value")
	jsonInvalidAt          = i18n.Define("json.decode.invalid_at", "{{path}}: the value could not be decoded")
	jsonInvalid            = i18n.Define("json.decode.invalid", "the value could not be decoded")

	jsonTypeString  = i18n.Define("json.type.string", "a string")
	jsonTypeNumber  = i18n.Define("json.type.number", "a number")
	jsonTypeBoolean = i18n.Define("json.type.boolean", "a boolean")
	jsonTypeObject  = i18n.Define("json.type.object", "an object")
	jsonTypeArray   = i18n.Define("json.type.array", "an array")
	jsonTypeNull    = i18n.Define("json.type.null", "null")
)

// JSONTrailingData is the JSONErrorDetail for a body holding more after
// its one JSON value, for a decode site with its own trailing check.
func JSONTrailingData() error { return i18n.NewError(jsonTrailingData) }

// Refusals shared by every route family that guards a write or a caller.
var (
	ReasonCrossSiteRefused       = i18n.Define("error.cross_site", "cross-site request refused")
	ReasonAuthenticationRequired = i18n.Define("error.authentication_required", "authentication required")
	ReasonMissingPermission      = i18n.Define("error.missing_permission", "access denied: missing permission {{permission}}")
)
