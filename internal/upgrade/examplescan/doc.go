// Package examplescan holds the check that the repo's migrated examples
// tree stays silent under the v0.86.0 and v0.87.0 release notes.
//
// Why it is its own package: the check reads every file under
// examples/ (its //gofastr:test-reads directive), so an edit to any
// example selects it. Beside the registry's other tests, each such edit
// reran every one of them; here it reruns one scan.
//
// The scan loads and type-checks the whole examples tree, the most
// expensive load in the upgrade tests, so it runs once per test binary
// and each release's assertions read the same result.
//
// This directory holds no production code.
package examplescan
