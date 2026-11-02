package appgen

import (
	"strings"
	"testing"
)

// An apiKey scheme's name is the header every request carries the token
// under, so it must be an HTTP field name (an RFC 9110 token). X-Key(1)
// passed the old check, which refused only spaces, quotes and
// backslashes, and the generated CLI then failed every request with
// "net/http: invalid header field name".
func TestOpenAPIAPIKeyHeaderMustBeAToken(t *testing.T) {
	doc := func(name string) map[string]any {
		return mustDecodeDoc(t, `{"openapi":"3.0.3",
		  "components":{"securitySchemes":{"k":{"type":"apiKey","in":"header","name":`+name+`}}},
		  "paths":{"/a":{"get":{"operationId":"getA"}}}}`)
	}
	for _, bad := range []string{`"X-Key(1)"`, `"X-Key:"`, `"X Key"`, `"Ключ"`, `"X-Key\u0000"`, `""`, `"a/b"`} {
		_, err := BuildOpenAPICLISpec(doc(bad), CliOptions{Binary: "x"}, "c")
		if err == nil || !strings.Contains(err.Error(), "plain header name") {
			t.Errorf("apiKey name %s: err = %v, want the header-name refusal", bad, err)
		}
	}
	for _, good := range []string{`"X-API-Key"`, `"x_api.key~1"`, `"Api-Key!#$%&'*+^|"`} {
		spec, err := BuildOpenAPICLISpec(doc(good), CliOptions{Binary: "x"}, "c")
		if err != nil {
			t.Errorf("apiKey name %s: %v", good, err)
			continue
		}
		if want := strings.Trim(good, `"`); spec.TokenHeader != want {
			t.Errorf("TokenHeader = %q, want %q", spec.TokenHeader, want)
		}
	}
}
