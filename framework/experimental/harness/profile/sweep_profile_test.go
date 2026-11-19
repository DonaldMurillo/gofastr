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

func TestParseRejectsTrailingContentAfterValue(t *testing.T) {
	const tail = "name = \"x\"\ndefault_model = \"m\"\n"
	for _, line := range []string{
		"schema_version = 1.5\n",
		"schema_version = 1 2\n",
		"schema_version = 1\nname = \"x\" extra\ndefault_model = \"m\"\n",
		"schema_version = 1\nallow_project_hooks = truex\n",
		"schema_version = 1\ntool_packs = [\"a\"] b\n",
	} {
		if _, err := Parse(strings.NewReader(line + tail)); err == nil {
			t.Errorf("Parse accepted trailing content in %q", line)
		}
	}
	p, err := Parse(strings.NewReader("schema_version = 1 # c\nname = \"x\"\t# n\ndefault_model = \"m\"   \ntool_packs = [\"a\"] # t\n"))
	if err != nil {
		t.Fatalf("Parse refused a value followed by whitespace and a comment: %v", err)
	}
	if p.SchemaVersion != 1 || p.Name != "x" || len(p.ToolPacks) != 1 {
		t.Fatalf("parsed %+v", p)
	}
}

func TestParseRejectsMCPServerKeyOfWrongType(t *testing.T) {
	for key, val := range map[string]string{
		"name":      `["a"]`,
		"cmd":       `["a"]`,
		"sha256":    `["a"]`,
		"discovery": `["eager"]`,
		"args":      `"x"`,
	} {
		entry := `name = "a", cmd = "a"`
		if key == "name" || key == "cmd" {
			entry = `name = "a"`
			if key == "name" {
				entry = `cmd = "a"`
			}
		}
		src := "schema_version = 1\nname = \"x\"\ndefault_model = \"m\"\nmcp_servers = [{ " + entry + ", " + key + " = " + val + " }]\n"
		_, err := Parse(strings.NewReader(src))
		if err == nil || !strings.Contains(err.Error(), "mcp_servers key \""+key+"\" must be") {
			t.Errorf("%s = %s: err = %v, want a type error naming %s", key, val, err, key)
		}
	}
}
