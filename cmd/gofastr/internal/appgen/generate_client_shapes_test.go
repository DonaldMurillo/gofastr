package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

func TestCommentSafeFlattensLineTerminators(t *testing.T) {
	for _, in := range []string{"a\nb", "a\rb", "a\u2028b", "a\u2029b", "a\x00b"} {
		if got := goCommentSafe(in); got != "a b" {
			t.Errorf("goCommentSafe(%q) = %q, want %q", in, got, "a b")
		}
	}
	if got := tsCommentSafe("a*/b\u2028"); strings.Contains(got, "*/") || strings.ContainsRune(got, '\u2028') {
		t.Errorf("tsCommentSafe left a comment end or line terminator: %q", got)
	}
}

func TestCreateInputSendsOnlyRequiredFieldsWithoutDefault(t *testing.T) {
	for _, tc := range []struct {
		f    framework.FieldDeclaration
		want bool
	}{
		{framework.FieldDeclaration{Name: "a", Type: "string", Required: true}, true},
		{framework.FieldDeclaration{Name: "b", Type: "string", Required: true, Default: "draft"}, false},
		{framework.FieldDeclaration{Name: "c", Type: "bool"}, false},
	} {
		if got := inputTakesValue(tc.f); got != tc.want {
			t.Errorf("inputTakesValue(%+v) = %v, want %v", tc.f, got, tc.want)
		}
	}
}
