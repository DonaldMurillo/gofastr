package webbotauth

import "testing"

// A Decimal re-serializes by RFC 9651 section 4.1.5: its own digits, the
// fraction without trailing zeros. The sf and key parameters put this
// text in the signature base, so "1.500" for a signer's "1.5" fails a
// good signature, and 1.005 read through a float came back as 1.004.
func TestSFDecimalSerializesCanonically(t *testing.T) {
	cases := map[string]string{
		"1.5":      "1.5",
		"1.50":     "1.5",
		"2.0":      "2.0",
		"1.005":    "1.005",
		"-0.5":     "-0.5",
		"-1.005":   "-1.005",
		"0.29":     "0.29",
		"4.350":    "4.35",
		"0.001":    "0.001",
		"a;q=0.9":  "a;q=0.9",
		"u=1.25":   "u=1.25",
		"1.0, 2.5": "1.0, 2.5",
	}
	for in, want := range cases {
		got, err := strictSerializeField(in)
		if err != nil || got != want {
			t.Errorf("strictSerializeField(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	d, err := parseSFDictionary("w=0.125, x=1.500")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := serializeMemberValue(d.get("x")); got != "1.5" {
		t.Errorf("member x = %q, want 1.5", got)
	}
}
