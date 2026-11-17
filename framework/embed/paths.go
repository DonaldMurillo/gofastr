package embed

import (
	"net/http"
	"path"
	"strings"
)

// The embed HTTP routes.
const (
	LoaderPath   = "/__gofastr/embed.js"
	RuntimePath  = "/__gofastr/embed-runtime.js"
	ExchangePath = "/__gofastr/embed-exchange"
	RefreshPath  = "/__gofastr/embed-refresh"
	// SurfacePrefix is the shell and content space, /__gofastr/embed/{surface}[/content].
	SurfacePrefix = "/__gofastr/embed/"

	// GrantHeader carries the frame's grant on every credentialed request.
	GrantHeader = "X-Gofastr-Embed"
)

// CSRFExempt reports whether r must not be gated on a CSRF token: it carries a
// grant header, targets the exchange or refresh endpoint, or is a safe-method
// request under SurfacePrefix.
func CSRFExempt(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}
	if r.Header.Get(GrantHeader) != "" {
		return true
	}
	clean := path.Clean(r.URL.Path)
	switch clean {
	case ExchangePath, RefreshPath:
		return true
	}
	if isSafeMethod(r.Method) && strings.HasPrefix(clean, SurfacePrefix) {
		return true
	}
	return false
}

func isSafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions, "":
		return true
	}
	return false
}
