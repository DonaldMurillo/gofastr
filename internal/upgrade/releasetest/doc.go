// Package releasetest drives the shipped release notes
// (internal/upgrade/releases/*.yml) through the real scan engine: each
// note's affected spelling is a hit, its migrated spelling is not, and
// its Go-API matchers resolve against a stub kit at the real import
// paths. This package covers v0.86.0 onward, one file (or a few) per
// release, and a new release's note tests go here. The releases before
// it are frozen eras in subpackages: old (v0.3.0–v0.55.0) and mid
// (v0.56.0–v0.85.0).
//
// Why these tests are not in internal/upgrade: each scans fixture apps
// with the go tool, while the registry package's own tests are parser unit
// tests and the release-process gates, one of which reads CHANGELOG.md.
// In one package, every CHANGELOG edit reran every scan. Here a change
// selects these tests when it can change a scan's outcome: the registry
// (its parser or any release file, because every test reads the whole
// embedded registry), the scanner, scantest, or the test's own
// directory. The eras share no code, so an edit to one era's fixtures
// reruns that era alone. The examples-tree silence check is its own
// package, internal/upgrade/examplescan: an examples edit selects that
// scan and nothing here.
//
// This directory holds no production code.
package releasetest
