package main

import (
	"bytes"
	"strings"
	"testing"
)

//gofastr:test-reads canon/ *.go *.md
func TestCommands(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{nil, 0, "crud: "},
		{[]string{"for", "framework/crud/nested_filter.go"}, 0, "crud.relations.nested-filter: "},
		{[]string{"for", "go.mod"}, 0, "no primitive owns this path yet"},
		{[]string{"show", "crud"}, 0, "child crud.scope: "},
		{[]string{"show", "crud.read-hooks"}, 0, "rejected Match by position: "},
		{[]string{"show", "nope"}, 2, "usage: canon show"},
		{[]string{"lint"}, 0, "clean"},
		{[]string{"gen", "--check"}, 0, ""},
		{[]string{"bogus"}, 2, `unknown command "bogus"`},
	} {
		var stdout, stderr bytes.Buffer
		code := run("../..", tc.args, &stdout, &stderr)
		out := stdout.String() + stderr.String()
		if code != tc.code || !strings.Contains(out, tc.want) {
			t.Errorf("canon %v: exit %d, want %d and output containing %q, got:\n%s", tc.args, code, tc.code, tc.want, out)
		}
	}
}
