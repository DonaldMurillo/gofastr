package uihost

import (
	"net/url"
	"testing"
)

func TestSwapEscapesControlBytes(t *testing.T) {
	for character := byte(0); character <= 0x7f; character++ {
		if character >= 0x20 && character != 0x7f {
			continue
		}
		key := "g:/projects/a" + string(character) + "b%é/:project"
		encoded := escapeLayerKey(key)
		for _, value := range []byte(encoded) {
			if value < 0x20 || value >= 0x7f {
				t.Errorf("header for %q contains unsafe byte %#x: %q", key, value, encoded)
			}
		}
		if decoded, err := url.PathUnescape(encoded); err != nil || decoded != key {
			t.Errorf("header %q decodes to %q (%v), want %q", encoded, decoded, err, key)
		}
	}
	host := fromParamHost(t)
	for _, value := range []byte{0x01, 0x07, 0x09, 0x1f, 0x7f} {
		project := "a" + string(value) + "b"
		path := "/projects/" + url.PathEscape(project)
		response := partialGet(t, host, path+"/issues/1", path)
		got := response.Header().Get("X-Gofastr-Swap")
		want := "g:/projects/" + url.PathEscape(project) + "/:project"
		if got != want {
			t.Errorf("swap header = %q, want %q", got, want)
		}
	}
}
