package clitest

import (
	"os"
	"strings"
	"testing"
)

// VariantBuild skips a test that builds a generated app unless
// GOFASTR_TEST_ALL=1 or GOFASTR_AFFECTED_FILES names one of its inputs, a
// repo-relative file or a directory prefix ending in "/". Call it right
// before the toolchain step.
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
