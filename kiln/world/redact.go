package world

import "github.com/DonaldMurillo/gofastr/internal/dsnredact"

// RedactedSecret is what a credential renders as in any world kiln hands
// out. A fixed marker rather than an empty string, so a reader can tell
// "configured, withheld" from "not set".
const RedactedSecret = "[redacted]"

// Redacted returns a shallow copy of w with its credentials masked: the
// JWT secret, the admin seed password, and a DB URL that embeds
// credentials. It is the one masking rule for every surface that returns
// the world to a caller (GET /kiln/world, /kiln/status, and the
// world_get tool on HTTP, MCP and ACP). The in-process world keeps the
// real values, which freeze reads from the replayed journal.
func Redacted(w *World) *World {
	if w == nil {
		return nil
	}
	out := *w
	if out.App.Auth.JWTSecret != "" {
		out.App.Auth.JWTSecret = RedactedSecret
	}
	if out.App.Admin.SeedPassword != "" {
		out.App.Admin.SeedPassword = RedactedSecret
	}
	// A credentialed DSN is credential material by the freeze contract
	// (dsnredact.HasSecret is that one rule): an embedded password must
	// not be handed out with the world, while credential-free DSNs
	// (file:blog.db) are configuration and stay visible so local frozen
	// apps stay diagnosable.
	if dsnredact.HasSecret(out.App.DBURL) {
		out.App.DBURL = RedactedSecret
	}
	return &out
}
