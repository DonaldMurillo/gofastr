package codetext

import (
	"strings"
	"testing"
)

func TestSweepCoreEscapedNewlineOpensALiteralComment(t *testing.T) {
	if got, want := Strip(`s := "a\n// note\n"`, false), `s := "a\n"`; got != want {
		t.Errorf("Strip = %q, want %q", got, want)
	}
}

func TestSweepCoreTabAfterSlashesOpensALiteralComment(t *testing.T) {
	if got := Strip("s := \"//\tnote\"", false); strings.Contains(got, "note") {
		t.Errorf("a tab after // starts comment text: %q", got)
	}
}

func TestSweepCoreUnterminatedQuoteEndsAtNewline(t *testing.T) {
	cs := Comments("s := \"a // c\ny := 1 // real\n", false)
	if len(cs) != 1 || cs[0].Text != "// real" || cs[0].Line != 2 {
		t.Fatalf("the newline closes an unterminated quote, got %+v", cs)
	}
}

func TestSweepCoreOpenQuoteDoesNotSwallowTheFile(t *testing.T) {
	cs := Comments("s := \"open\nx := 1 // real\n", false)
	if len(cs) != 1 || cs[0].Text != "// real" {
		t.Fatalf("an unterminated quote ran past its line: %+v", cs)
	}
}
