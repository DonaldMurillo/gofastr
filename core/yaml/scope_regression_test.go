package yaml

import "testing"

func TestMappingColonRespectsQuotesAndSeparation(t *testing.T) {
	n, e := Parse("\"host:port\": ok\n")
	if e != nil || n.Map["host:port"].Value != "ok" {
		t.Fatalf("quoted key: %v %v", n, e)
	}
	n, e = Parse("- https://example.com\n")
	if e != nil || n.List[0].Kind != Scalar || n.List[0].Value != "https://example.com" {
		t.Fatalf("URL list: %v %v", n, e)
	}
}
func TestMappingKeepsPlainApostropheKeys(t *testing.T) {
	n, e := Parse("owner's: ok\n")
	if e != nil {
		t.Fatal(e)
	}
	if n.Map["owner's"].Value != "ok" {
		t.Fatal(n)
	}
}

func TestSingleQuotedScalarsRejectInteriorQuote(t *testing.T) {
	if _, e := Parse("key: 'a'b'\n"); e == nil {
		t.Fatal("invalid single quoted scalar accepted")
	}
}
func TestTabInsideQuotedValue(t *testing.T) {
	n, e := Parse("key: \"a\tb\"\n")
	if e != nil {
		t.Fatal(e)
	}
	if n.Map["key"].Value != "a\tb" {
		t.Fatal(n.Map["key"].Value)
	}
}

func TestMappingKeysRejectDecodedControls(t *testing.T) {
	for _, input := range []string{`"a\nb": x`, `"a\u0000b": x`, "a\tb: x", "a\rb: x"} {
		if _, e := Parse(input); e == nil {
			t.Errorf("accepted control key in %q", input)
		}
	}
}
