package webbotauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

// parseJWK threw away the decode errors for use, alg and kid, so
// "use":[] or "alg":false read as absent and the key passed both
// restriction checks. A present field that is not a string is a
// malformed key, refused like a bad x.
func TestJWKMalformedFieldsRefused(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	x := base64.RawURLEncoding.EncodeToString(pub)
	key := func(extra string) []byte {
		return []byte(`{"keys":[{"kty":"OKP","crv":"Ed25519","x":"` + x + `"` + extra + `}]}`)
	}
	for _, extra := range []string{`,"use":[]`, `,"alg":false`, `,"kid":5`, `,"use":null`} {
		if _, err := parseJWKS(key(extra)); err == nil {
			t.Errorf("parseJWKS with %s: accepted, want a malformed-key refusal", extra)
		}
	}
	set, err := parseJWKS(key(`,"use":"sig","alg":"EdDSA","kid":"k1"`))
	if err != nil || len(set.keys) != 1 {
		t.Fatalf("well-formed key: set %v, err %v", set, err)
	}
	set, err = parseJWKS(key(`,"use":"enc"`))
	if err != nil || len(set.keys) != 0 {
		t.Fatalf("use enc must be skipped, not refused: set %v, err %v", set, err)
	}
	if _, err := parseJWKS(key(`,"alg":1`)); err == nil || !strings.Contains(err.Error(), "alg") {
		t.Fatalf("error must name the field: %v", err)
	}
	// A non-OKP key is skipped whatever its other members hold.
	set, err = parseJWKS([]byte(`{"keys":[{"kty":"RSA","use":[]}]}`))
	if err != nil || len(set.keys) != 0 {
		t.Fatalf("foreign key type: set %v, err %v", set, err)
	}
}
