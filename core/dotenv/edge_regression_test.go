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

func TestQuotedValuesRejectUnescapedInteriorQuotes(t *testing.T) {
	for _, input := range []string{`KEY="one"two"`, `KEY='one'two'`} {
		if _, err := Parse(strings.NewReader(input)); err == nil {
			t.Errorf("accepted malformed quoted value %s", input)
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
