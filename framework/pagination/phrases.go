package pagination

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals a cursor decode returns.
var (
	reasonCursorTooLong       = i18n.Define("pagination.cursor_too_long", "pagination: cursor exceeds {{max}} bytes")
	reasonCursorToken         = i18n.Define("pagination.cursor_token", "pagination: cursor token: {{detail}}")
	reasonCursorTooManyFields = i18n.Define("pagination.cursor_too_many_fields", "pagination: cursor carries {{count}} fields (max {{max}})")
)
