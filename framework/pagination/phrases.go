package pagination

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals a cursor decode, or NewCursorPage, returns.
var (
	reasonCursorTooLong       = i18n.Define("pagination.cursor_too_long", "pagination: cursor exceeds {{max}} bytes")
	reasonCursorToken         = i18n.Define("pagination.cursor_token", "pagination: cursor token: {{detail}}")
	reasonCursorTooManyFields = i18n.Define("pagination.cursor_too_many_fields", "pagination: cursor carries {{count}} fields (max {{max}})")
	reasonCursorValueTooLong  = i18n.Define("pagination.cursor_value_too_long", "pagination: the last row's {{field}} makes a {{size}}-byte cursor (max {{max}})")
	reasonCursorKeyNull       = i18n.Define("pagination.cursor_key_null", "pagination: the last row's {{field}} is null, so no cursor can resume after it")
	reasonCursorKeyMissing    = i18n.Define("pagination.cursor_key_missing", "pagination: the last row has no {{field}} to make a cursor from")
)
