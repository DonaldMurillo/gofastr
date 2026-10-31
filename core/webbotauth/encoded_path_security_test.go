package webbotauth

import (
	"net/http/httptest"
	"testing"
)

// RFC 9421 section 2.2.6: @path is the request's path before any
// percent-encoded octet is decoded, and @request-target and @target-uri
// carry the same path. The base read r.URL.Path, the decoded form: a bot
// signing /docs/a%20b failed to verify here, and a signature over /a/b
// also verified on /a%2Fb, a path ServeMux routes elsewhere.
func TestBaseSignsEncodedPath(t *testing.T) {
	const input = `sig1=("@path" "@request-target" "@target-uri");created=1735689600;keyid="k"`
	dict, err := parseSFDictionary(input)
	if err != nil {
		t.Fatal(err)
	}
	m := dict.get("sig1")
	base := func(target string) string {
		t.Helper()
		r := httptest.NewRequest("GET", target, nil)
		r.Host = "example.com"
		b, err := buildSignatureBase(newRequestCtx(r), m.list.items, m.list.params)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	params := `"@signature-params": ("@path" "@request-target" "@target-uri");created=1735689600;keyid="k"`
	for _, c := range []struct{ target, path, rest string }{
		{target: "/docs/a%20b?x=1", path: "/docs/a%20b", rest: "?x=1"},
		{target: "/a%2Fb", path: "/a%2Fb"},
		// The octets as sent: the hex case and an encoded unreserved
		// character stay as the client wrote them.
		{target: "/a%2fb", path: "/a%2fb"},
		{target: "/a%41", path: "/a%41"},
		{target: "/plain?q", path: "/plain", rest: "?q"},
	} {
		want := `"@path": ` + c.path + "\n" +
			`"@request-target": ` + c.path + c.rest + "\n" +
			`"@target-uri": http://example.com` + c.path + c.rest + "\n" +
			params
		if got := base(c.target); got != want {
			t.Errorf("%s: base\n got: %q\nwant: %q", c.target, got, want)
		}
	}
	if base("/a/b") == base("/a%2Fb") {
		t.Error("/a/b and /a%2Fb sign the same base, so one signature verifies on both routes")
	}
}
