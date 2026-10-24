package handler

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The request-binding refusals, under "error.<code>". A detail param
// carries the decoder's own (English) description of what it found.
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

// Refusals shared by every route family that guards a write or a caller.
var (
	ReasonCrossSiteRefused       = i18n.Define("error.cross_site", "cross-site request refused")
	ReasonAuthenticationRequired = i18n.Define("error.authentication_required", "authentication required")
	ReasonMissingPermission      = i18n.Define("error.missing_permission", "access denied: missing permission {{permission}}")
)
