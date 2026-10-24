package embed

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals this package writes, under their catalog keys.
var (
	reasonReservedPath     = i18n.Define("embed.reserved_path", "embed surface {{surface}} may not reach {{path}}: {{reserved}} is mounted by the framework or a battery and no embed grant may reach it")
	reasonOutsideReach     = i18n.Define("embed.outside_reach", "embed surface {{surface}} may not reach {{path}}: add it to that surface's Reach to allow it, e.g. Reach: {{suggestion}}")
	reasonMisordered       = i18n.Define("embed.misordered", "embed: Middleware() ran after something that already resolved {{what}}: install it OUTERMOST, before session/bearer/API-token/tenant middleware, so the grant's subject is the only identity on the request")
	reasonPathNotCanonical = i18n.Define("embed.path_not_canonical", "embed: request path is not canonical")
)
