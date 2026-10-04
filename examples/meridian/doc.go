// Package main is Meridian, the GoFastr flagship demo, and the
// design-system completeness canary (CLAUDE.md hard rule 9).
//
// # Maintenance model: two generator-owned trees, the rest hand-maintained
//
// Meridian was SEEDED by `gofastr generate --from=gofastr.yml` and then
// hand-evolved. Two of the trees that seed produced are now back under
// generator ownership, gated for byte drift:
//
//   - entities/ (from `gofastr generate --from=gofastr.yml`)
//   - cmd/meridian/ (from `gofastr generate cli --binary=meridian`)
//
// blueprint_gate_test.go regenerates both into a scratch package and
// asserts the committed files match the fresh output byte for byte
// (TestGeneratedTreesMatchGenerator), so a template change that is not
// followed by a regeneration is a red test, not silent drift. The one
// exception is cmd/meridian/custom.go: it is the dev-owned extension
// seam, the generator only creates it when absent, so it is excluded
// from the comparison. Hand-written code must not be added under the
// two owned trees; it belongs in the hand-maintained part of the app.
//
// Everything else in this package is hand-maintained: inkTheme,
// appIconPNG, the sdkdocs mount, ResourceConfig's ExtraActions
// / WithIsland / TableHandler, the quick-add customer modal, the
// keyboard / visual / API-token test suites, and every screen and app
// file at the root. For those surfaces `gofastr generate --force` must
// never be run in this directory: it would clobber hand-written code
// the generator does not emit. To refresh the two owned trees, generate
// into a scratch directory (the gate test shows the exact steps) and
// copy entities/ and cmd/meridian/ back, keeping custom.go.
//
// `gofastr generate` is a one-shot scaffolder for everything it emits
// beyond the two owned trees: that code was yours to own from the first
// byte, and the generator has no regen-and-merge mode for it. Teaching
// it to reproduce a hand-evolved flagship would turn the scaffolder
// into a code manager, which is a different product.
//
// The two example apps therefore play different roles, and it is worth
// keeping them straight:
//
//   - examples/ecommerce is the GENERATOR fixture. Its blueprint sets
//     output_dir: app, so the generator owns app/ outright, and
//     blueprint_gate_test.go asserts the committed app/ matches fresh
//     generator output. If the generator regresses, that test fails.
//
//   - examples/meridian (this app) is the DESIGN-SYSTEM fixture. It
//     proves that framework/ui + core-ui can carry a real product
//     across marketing, app, auth, admin, and mobile in both color
//     schemes, with zero bespoke CSS. Hand-editing the surfaces around
//     the two owned trees is the point, a surface here that needs CSS
//     the components don't provide is an upstream gap to fix, never a
//     local patch.
//
// # What still gates the blueprint
//
// gofastr.yml is not decoration, and it is gated from both directions.
//
// Forward (blueprint → code), by blueprint_gate_test.go in this
// package: it copies the blueprint into a scratch package, generates
// the app and the CLI with the in-tree generator, compiles the app
// (TestBlueprintStillGenerates), and compares the two owned trees
// against the committed files (TestGeneratedTreesMatchGenerator). The
// blueprint can therefore never rot into something that no longer
// produces a buildable app, and the owned trees can never drift from
// the templates. The rot gate is the lesson of #131; the drift gate is
// the lesson of #416.
//
// Backward (code → blueprint), by cmd/gofastr/pack_test.go: it runs
// `gofastr pack` over THIS directory and asserts the recovered
// declarations equal the parsed gofastr.yml. So the app's declarative
// surfaces, entities, screens, nav, seed, must still match the
// blueprint even though the hand-written Go around them does not.
//
// Notes that explain WHY a declaration is shaped the way it is live in
// gofastr.yml next to the declaration (for example, why plans has no
// owner_field and gates writes behind permissions), never in the
// generated files, so regeneration cannot strip them.
package main
