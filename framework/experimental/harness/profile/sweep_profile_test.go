package profile

import (
	"strings"
	"testing"
)

func TestParseRejectsUnknownInlineTableKey(t *testing.T) {
	src := "schema_version = 1\nname = \"x\"\ndefault_model = \"m\"\nmcp_servers = [{ name = \"a\", cmd = \"a\", sha265 = \"ab\" }]\n"
	_, err := Parse(strings.NewReader(src))
	if err == nil || !strings.Contains(err.Error(), "sha265") {
		t.Fatalf("err = %v, want an unknown-key error naming sha265", err)
	}
}

func TestParseRejectsTableHeader(t *testing.T) {
	src := "schema_version = 1\nname = \"x\"\ndefault_model = \"m\"\n[extra]\n"
	if _, err := Parse(strings.NewReader(src)); err == nil {
		t.Fatal("Parse accepted a [table] header")
	}
}
