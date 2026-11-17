package dev

// DevMCPEnabled reports whether the dev MCP surface should auto-activate:
// the livereload gate with GOFASTR_DEV_MCP as its opt-out.
func DevMCPEnabled() bool {
	return devFeatureEnabled("GOFASTR_DEV_MCP")
}
