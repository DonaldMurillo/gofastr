package log

import (
	"bytes"
	"strings"
	"testing"
)

func TestConsoleMalformedEntryIsEscaped(t *testing.T) {
	var buf bytes.Buffer
	if err := sinkFor(&buf, false).Write([]byte("not json \x1b[2J‮")); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(buf.String(), "\x1b‮") {
		t.Fatalf("a malformed entry reached the terminal raw: %q", buf.String())
	}
}
