package upload

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals this package writes, under their catalog keys.
var (
	reasonMIMENotAllowed = i18n.Define("upload.mime_not_allowed", "unsupported MIME type: {{type}} (allowed: {{allowed}})")
	reasonSizeExceeded   = i18n.Define("upload.size_exceeded", "file size {{size}} exceeds maximum {{max}} bytes")
	reasonNoExtension    = i18n.Define("upload.no_extension", "file has no extension")
	reasonExtNotAllowed  = i18n.Define("upload.ext_not_allowed", "file extension .{{ext}} not allowed (allowed: {{allowed}})")
	reasonFileTooLarge   = i18n.Define("upload.too_large", "file too large")
	reasonInvalidKey     = i18n.Define("upload.invalid_key", "invalid key")
	reasonMissingFile    = i18n.Define("upload.missing_file", "missing file field")
	reasonSniffFailed    = i18n.Define("upload.sniff_failed", "failed to sniff content")
)
