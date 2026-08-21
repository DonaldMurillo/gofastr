// Package main is Meridian, the GoFastr flagship demo, and the
// design-system completeness canary (CLAUDE.md hard rule 9).
//
// # Maintenance model: hand-maintained, blueprint-seeded
//
// Meridian was SEEDED by `gofastr generate --from=gofastr.yml` and has
// been hand-evolved ever since. It is NOT regenerable, and
// `gofastr generate --from=gofastr.yml --force` must never be run here:
// it would clobber hand-written surfaces the generator does not emit,
// inkTheme, appIconPNG, the sdkdocs mount, ResourceConfig's ExtraActions
// / WithIsland / TableHandler, the quick-add customer modal, and the
// keyboard / visual / API-token test suites.
//
// That is deliberate, not drift to be fixed. `gofastr generate` is a
// one-shot scaffolder: the code it emits is yours to own from the first
// byte, and it has no regen-and-merge mode. Teaching it to reproduce a
// hand-evolved flagship would turn the scaffolder into a code manager,
// which is a different product.
//
// The two example apps therefore play different roles, and it is worth
// keeping them straight:
//
//   - examples/ecommerce is the GENERATOR fixture. Its blueprint sets
//     output_dir: app, so the generator owns app/ outright and
//     flagship_test.go regenerates it with --force on every run. If the
//     generator regresses, that test fails.
//
//   - examples/meridian (this app) is the DESIGN-SYSTEM fixture. It
//     proves that framework/ui + core-ui can carry a real product across
//     marketing, app, auth, admin, and mobile in both color schemes,
//     with zero bespoke CSS. Hand-editing it is the point, a surface
//     here that needs CSS the components don't provide is an upstream
//     gap to fix, never a local patch.
//
// # What still gates the blueprint
//
// gofastr.yml is not decoration, and it is gated from both directions.
//
// Forward (blueprint → code), by blueprint_gate_test.go in this package:
// it copies the blueprint into a scratch package, generates it with the
// in-tree CLI, and compiles the result. The blueprint can therefore
// never rot into something that no longer produces a buildable app,
// even though that output no longer matches the files checked in here.
// This is the gate meridian was missing, its absence is how #131's
// drift accumulated unnoticed.
//
// Backward (code → blueprint), by cmd/gofastr/pack_test.go: it runs
// `gofastr pack` over THIS directory and asserts the recovered
// declarations equal the parsed gofastr.yml. So the app's declarative
// surfaces, entities, screens, nav, seed, must still match the
// blueprint even though the hand-written Go around them does not.
//
// The two gates together say something precise: the blueprint and this
// app agree on WHAT the product declares, and disagree only about the
// hand-written Go that renders it. That is the intended state.
//
// Files that came from the generator carry a provenance comment saying
// so. They are ordinary hand-maintained Go, not generated artifacts:
// edit them freely and do not expect a regeneration to reproduce them.
package main
