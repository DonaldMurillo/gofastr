package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

// JSONErrorDetail phrased any UnmarshalTypeError
// whose expected and received JSON types are both known as "expected X,
// got Y". encoding/json also reports a string that is the right JSON
// type but does not decode into the field as an UnmarshalTypeError (a
// []byte field given text that is not base64), so the client was told
// "expected a string, got a string": a refusal that names no fault. It
// now reads as a value that could not be decoded, at its path.
func TestJSONDetailInvalidBase64IsNotATypeMismatch(t *testing.T) {
	var dst struct {
		Blob []byte `json:"blob"`
	}
	err := json.Unmarshal([]byte(`{"blob":"!!! not base64 !!!"}`), &dst)
	if err == nil {
		t.Fatal("setup: encoding/json accepted the value")
	}
	got := JSONErrorDetail(err).Error()
	if strings.Contains(got, "expected a string, got a string") {
		t.Errorf("detail = %q: the value IS a string; the message must not claim a type mismatch between a type and itself", got)
	}
	if want := "blob: the value could not be decoded"; got != want {
		t.Errorf("detail = %q, want the could-not-be-decoded phrase at the path, %q", got, want)
	}
}
