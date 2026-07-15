package dev

import (
	"os"
	"strconv"
)

// DevMCPEnabled reports whether the dev-only MCP agent surface should
// auto-activate: the /mcp mount, the read-only introspection tools, the
// mutating control tools, and battery debug tools (battery/log) all key
// off this. Mirrors LiveReloadEnabled's gate:
//
//   - GOFASTR_ENV naming a production-like environment → off.
//   - GOFASTR_DEV must be ParseBool-truthy (set by `gofastr dev`).
//   - GOFASTR_DEV_MCP set to a falsy ParseBool value → opt-out even
//     when GOFASTR_DEV is set.
//   - Otherwise on.
//
// Rationale: `gofastr dev` means "a local, trusted loop where the
// primary consumer is the developer's own agent" — the same reasoning
// that auto-wires livereload. Production processes never see
// GOFASTR_DEV, so none of this activates there.
func DevMCPEnabled() bool {
	if isNonDevEnv(os.Getenv("GOFASTR_ENV")) {
		return false
	}
	if !envBool("GOFASTR_DEV") {
		return false
	}
	if v := os.Getenv("GOFASTR_DEV_MCP"); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil && !b {
			return false
		}
	}
	return true
}
