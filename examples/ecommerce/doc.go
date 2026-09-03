// Package ecommerce is GoFastr's declaration-driven flagship example: a
// complete storefront: five related entities, screens, navigation, custom
// endpoints, seed data, and a theme, declared once in gofastr.yml and
// emitted as runnable Go by the CLI:
//
//	gofastr generate --from=gofastr.yml
//
// The generated app is committed under app/ (output_dir: app in the
// blueprint) and is the proof of the framework's thesis: one blueprint
// produces a SQL schema, REST CRUD, an OpenAPI spec, a typed MCP tool
// surface, and a server-rendered UI, none of it hand-written.
// flagship_test.go regenerates app/ with --force on every run and
// blueprint_gate_test.go fails when the committed files differ from what
// the in-tree generator emits, so app/ can never drift from the CLI.
//
// Run it directly:
//
//	go run ./examples/ecommerce/app
package ecommerce
