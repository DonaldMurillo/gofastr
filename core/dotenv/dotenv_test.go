package dotenv_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/dotenv"
)

func TestParse_BasicKV(t *testing.T) {
	got, err := dotenv.Parse(strings.NewReader("FOO=bar\nBAZ=qux\n"))
	mustOK(t, err)
	mustEq(t, got["FOO"], "bar")
	mustEq(t, got["BAZ"], "qux")
}

func TestParse_BlankLinesAndComments(t *testing.T) {
	in := `
# leading comment
FOO=bar

# another
BAZ=qux # not stripped — inline-comment handling is shell-specific, skip
`
	got, err := dotenv.Parse(strings.NewReader(in))
	mustOK(t, err)
	mustEq(t, got["FOO"], "bar")
	// Inline # after value is part of the value when unquoted. Real
	// dotenv loaders disagree on this; we pick "no inline-comment
	// surgery" to keep the parser predictable.
	mustEq(t, got["BAZ"], "qux # not stripped — inline-comment handling is shell-specific, skip")
	if _, present := got["#"]; present {
		t.Fatalf("comment line leaked as key")
	}
}

func TestParse_DoubleQuotedWithEscapes(t *testing.T) {
	in := `MSG="hello\nworld\t\"q\"\\back"`
	got, err := dotenv.Parse(strings.NewReader(in))
	mustOK(t, err)
	mustEq(t, got["MSG"], "hello\nworld\t\"q\"\\back")
}

func TestParse_SingleQuotedIsLiteral(t *testing.T) {
	// Per shell convention, single-quoted values do NOT interpret
	// escapes. Backslashes pass through verbatim.
	in := `MSG='hello\nworld'`
	got, err := dotenv.Parse(strings.NewReader(in))
	mustOK(t, err)
	mustEq(t, got["MSG"], `hello\nworld`)
}

func TestParse_ExportPrefix(t *testing.T) {
	in := "export FOO=bar\n  export BAZ=qux"
	got, err := dotenv.Parse(strings.NewReader(in))
	mustOK(t, err)
	mustEq(t, got["FOO"], "bar")
	mustEq(t, got["BAZ"], "qux")
}

func TestParse_LeadingTrailingWhitespace(t *testing.T) {
	in := "  FOO  =  bar  \n"
	got, err := dotenv.Parse(strings.NewReader(in))
	mustOK(t, err)
	// Key whitespace stripped; value whitespace OUTSIDE quotes stripped.
	mustEq(t, got["FOO"], "bar")
}

func TestParse_EmptyValue(t *testing.T) {
	got, err := dotenv.Parse(strings.NewReader("FOO="))
	mustOK(t, err)
	v, ok := got["FOO"]
	if !ok {
		t.Fatalf("FOO missing")
	}
	mustEq(t, v, "")
}

func TestParse_RejectsMalformed(t *testing.T) {
	cases := []string{
		"NO_EQUALS",
		"=novalue",                   // empty key
		"123ABC=startswithdigit",     // illegal key
		`UNTERMINATED="oops`,         // unclosed double-quote
		`UNTERMINATED2='oops`,        // unclosed single-quote
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if _, err := dotenv.Parse(strings.NewReader(in)); err == nil {
				t.Fatalf("expected parse error for %q", in)
			}
		})
	}
}

func TestParse_DuplicateKeyLastWins(t *testing.T) {
	got, err := dotenv.Parse(strings.NewReader("FOO=one\nFOO=two\n"))
	mustOK(t, err)
	mustEq(t, got["FOO"], "two")
}

func mustOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func mustEq(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
