package widget

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals this package writes, under their catalog keys.
var (
	reasonRuntimeUnavailable = i18n.Define("widget.runtime_unavailable", "runtime unavailable")
	reasonInvalidChrome      = i18n.Define("widget.invalid_chrome", "invalid chrome context")
	reasonWidgetNeedsSession = i18n.Define("widget.session_required", "forbidden: widget requires a session")
)
