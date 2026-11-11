package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// encoding/json replaces a malformed byte with U+FFFD, so Bind and
// UnmarshalStrict handed the caller text the client never sent. JSON
// text is UTF-8 (RFC 8259): a body that is not is refused before any
// decode.
func TestJSONBodyMustBeUTF8(t *testing.T) {
	type dst struct {
		Title string `json:"title"`
	}
	bad := "{\"title\":\"a\xff\xfeb\"}"

	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(bad))
	req.Header.Set("Content-Type", "application/json")
	var bound dst
	if err := Bind(req, &bound); !isReason(err, ReasonJSONNotUTF8) {
		t.Errorf("Bind = %v (title %q), want json_not_utf8", err, bound.Title)
	}
	var strict dst
	if err := UnmarshalStrict([]byte(bad), &strict); !isReason(err, ReasonJSONNotUTF8) {
		t.Errorf("UnmarshalStrict = %v (title %q), want json_not_utf8", err, strict.Title)
	}
	var m map[string]any
	if err := DecodeStrict(strings.NewReader(bad), &m); !isReason(err, ReasonJSONNotUTF8) {
		t.Errorf("DecodeStrict = %v, want json_not_utf8", err)
	}
	if err := UnmarshalStrict([]byte(`{"title":"café 😀"}`), &strict); err != nil || strict.Title != "café 😀" {
		t.Errorf("valid UTF-8 = %v %q", err, strict.Title)
	}
}

func isReason(err error, want interface{ Code() string }) bool {
	e, ok := err.(*Error)
	return ok && e.Code == http.StatusBadRequest && e.Reason.Code() == want.Code()
}
