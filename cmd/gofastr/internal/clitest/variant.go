package clitest

import (
	"os"
	"strings"
	"testing"
)

// VariantBuild gates a test that generates an app and compiles it: a
// shipped example, a small feature set, one generator's output. Each such
// build compiles most of the framework into a new binary. A pull request
// builds one generated app, appgen's TestUnionApp, whose app spec uses
// every generator feature that fits in one app; the others are variants.
//
// A variant runs when:
//
//   - GOFASTR_TEST_ALL=1: every CI run but a pull request's (pushes to
//     main, the merge queue, the scheduled runs), cmd/affected when it
//     tests every package, and scripts/test-all.sh;
//   - the change set cmd/affected hands the test command in
//     GOFASTR_AFFECTED_FILES touches one of its inputs, so a pull request
//     that edits an example's app spec still builds that example. An input
//     is a repo-relative file path, or a directory prefix ending in "/".
//
// Otherwise it skips. A variant with no inputs runs only in the first
// case, and it never reads the change set: Go keys a package's cached
// test results on every variable its tests read, and the change set
// differs from run to run. Call it right before the toolchain step, after
// the test's in-process checks, so those still run on a pull request.
func VariantBuild(t testing.TB, inputs ...string) {
	t.Helper()
	const skip = "variant app build: runs under GOFASTR_TEST_ALL=1 or when the change touches its inputs; appgen's TestUnionApp is the pull-request build"
	if os.Getenv("GOFASTR_TEST_ALL") == "1" {
		return
	}
	if len(inputs) == 0 {
		t.Skip(skip)
	}
	for f := range strings.SplitSeq(os.Getenv("GOFASTR_AFFECTED_FILES"), "\n") {
		if f == "" {
			continue
		}
		for _, in := range inputs {
			if f == in || strings.HasSuffix(in, "/") && strings.HasPrefix(f, in) {
				return
			}
		}
	}
	t.Skip(skip)
}
