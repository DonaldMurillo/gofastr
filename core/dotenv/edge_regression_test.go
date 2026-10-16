package dotenv

import (
	"strings"
	"testing"
)

func TestKeysFollowASCIIGrammar(t *testing.T) {
	for _, key := range []string{"é", "A١", "字"} {
		if _, err := Parse(strings.NewReader(key + "=value")); err == nil {
			t.Errorf("accepted non-ASCII key %q", key)
		}
	}
}

// The outer quotes delimit the value; a quote inside it is part of the
// value, as it was before (dotenv.md: single quotes are verbatim).
func TestInteriorQuotesStayInValue(t *testing.T) {
	cases := map[string]string{
		`KEY='it''s'`:   `it''s`,
		`KEY='one'two'`: `one'two`,
		`KEY="one"two"`: `one"two`,
	}
	for input, want := range cases {
		got, err := Parse(strings.NewReader(input))
		if err != nil {
			t.Errorf("%s: %v", input, err)
			continue
		}
		if got["KEY"] != want {
			t.Errorf("%s: KEY = %q, want %q", input, got["KEY"], want)
		}
	}
}

func TestParserRejectsNULValues(t *testing.T) {
	if _, err := Parse(strings.NewReader("KEY=value\x00suffix")); err == nil {
		t.Fatal("NUL value accepted even though process environment cannot store it")
	}
}

func TestInvalidKeyErrorsDoNotEchoSecrets(t *testing.T) {
	_, err := Parse(strings.NewReader("PASSWORD hunter2 = typo"))
	if err == nil {
		t.Fatal("invalid key accepted")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("parse error discloses secret-bearing malformed key: %v", err)
	}
}
