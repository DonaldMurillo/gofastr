package main

import "testing"

func TestSelectorStatusSkipsCommentLines(t *testing.T) {
	body := []byte("   # [ -n \"$(go run ./cmd/affected)\" ]\n")
	if got := selectorStatusFindings("build.sh", body); len(got) != 0 {
		t.Fatalf("linted a comment: %v", got)
	}
}
func TestShardNamesKeepsPackageRoot(t *testing.T) {
	for _, fields := range [][]string{{"./..."}, {"./"}, {"."}} {
		if !shardNames(fields, ".") {
			t.Errorf("%q does not match the root", fields)
		}
	}
	if !shardNames([]string{"./..."}, "foo/bar") {
		t.Error("./... did not match a subpackage")
	}
}
func TestModulePathAcceptsGoModWhitespace(t *testing.T) {
	if got := moduleLine([]byte("module\texample.com/thing // comment\n")); got != "example.com/thing" {
		t.Fatalf("module path = %q", got)
	}
}
