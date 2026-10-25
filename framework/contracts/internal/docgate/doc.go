// Package docgate holds the check that README.md and CHANGELOG.md state
// the contract catalog's rule count. It lives apart from framework and
// framework/contracts for two reasons: every pull request edits
// CHANGELOG.md, so the package that reads it should be small, and
// framework/contracts' own tests register extra rules, so the count is
// only the catalog's in a test binary that links nothing else.
package docgate
