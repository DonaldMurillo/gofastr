package pagination

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// A cursor token whose JSON fails to decode answers
// pagination.cursor_token with handler.JSONErrorDetail's phrase as the
// {{detail}}: the key refusal, or the JSON path and type. The decoder's
// own text named the token's Go struct ("Go struct field cursorToken.f
// of type string") and a string detail does not translate.
func TestCursorTokenDecodeDetailIsPhrased(t *testing.T) {
	enc := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	decodeSingle := func(c string) error { _, _, err := DecodeCursor(c); return err }
	decodeMulti := func(c string) error { _, err := DecodeMultiCursor(c); return err }
	for _, tc := range []struct {
		name   string
		decode func(string) error
		token  string
		want   string
	}{
		{"single wrong type", decodeSingle, `{"f":5}`, "pagination: cursor token: f: expected a string, got a number"},
		{"single folded keys", decodeSingle, `{"f":"a","F":"b"}`, `pagination: cursor token: invalid JSON: keys "f" and "F" name the same field`},
		{"multi wrong type", decodeMulti, `{"f":[{"n":1}]}`, "pagination: cursor token: f[0].n: expected a string, got a number"},
		{"multi folded keys", decodeMulti, `{"f":[{"n":"a","N":"b"}]}`, `pagination: cursor token: invalid JSON: keys "n" and "N" in f[0] name the same field`},
	} {
		err := tc.decode(enc(tc.token))
		var ie *i18n.Error
		if !errors.As(err, &ie) || ie.Reason != reasonCursorToken {
			t.Fatalf("%s: %v, want pagination.cursor_token", tc.name, err)
		}
		if _, ok := ie.Params["detail"].(i18n.Phrased); !ok {
			t.Fatalf("%s: detail %T %q is not a phrase", tc.name, ie.Params["detail"], ie.Params["detail"])
		}
		if err.Error() != tc.want {
			t.Fatalf("%s: %q, want %q", tc.name, err, tc.want)
		}
		if strings.Contains(err.Error(), "Go ") || strings.Contains(err.Error(), "cursorToken") {
			t.Fatalf("%s: %q names a Go type", tc.name, err)
		}
	}
}
