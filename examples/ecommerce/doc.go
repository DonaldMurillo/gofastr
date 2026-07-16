// Package ecommerce is GoFastr's declaration-driven flagship example: a
// complete storefront — five related entities, screens, navigation, custom
// endpoints, seed data, and a theme — declared once in gofastr.yml and
// emitted as runnable Go by the CLI:
//
//	gofastr generate --from=gofastr.yml
//
// The generated app lives under gen/ (gitignored — regenerate with the command
// above; flagship_test.go regenerates it on every run) and is the proof of the
// framework's thesis: one blueprint produces a SQL schema,
// REST CRUD, an OpenAPI spec, a typed MCP tool surface, and a server-rendered
// UI — none of it hand-written. See flagship_test.go for the end-to-end
// surface check.
//
// The scaffolded, owned app lives in the app/ subpackage (output_dir: app);
// run it directly:
//
//	go run ./examples/ecommerce/app
package ecommerce
