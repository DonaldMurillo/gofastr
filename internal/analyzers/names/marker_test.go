package names

import (
	"reflect"
	"testing"
)

func TestParseMarker(t *testing.T) {
	for _, c := range []struct {
		comment string
		want    Marker
		ok      bool
	}{
		{"// prose mentions gofastr:allow(rootwrite) here", Marker{}, false},
		{"/*\n gofastr:allow(rootwrite, rootread\tlayerprefix) — justified\n*/", Marker{Rules: []string{"rootwrite", "rootread", "layerprefix"}, Reason: "justified"}, true},
		{"/* gofastr:allow(rootwrite) — : */", Marker{Rules: []string{"rootwrite"}}, true},
		{"//gofastr:allow(rootwrite) -- */", Marker{Rules: []string{"rootwrite"}, Reason: "*/"}, true},
		{"/* gofastr:allow-file(rendering/unknown-theme-token) vendored */", Marker{File: true, Rules: []string{"rendering/unknown-theme-token"}, Reason: "vendored"}, true},
		{"//gofastr:allow()", Marker{}, true},
	} {
		if got, ok := ParseMarker(c.comment); ok != c.ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseMarker(%q) = %#v, %v; want %#v, %v", c.comment, got, ok, c.want, c.ok)
		}
	}
}
