package uinoderender

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The empty state drawn for a data table that arrives with no columns.
var (
	pageNoColumns     = i18n.Define("uinoderender.no_columns", "No columns")
	pageNoColumnsBody = i18n.Define("uinoderender.no_columns_body", "This table has no columns to display.")
)
