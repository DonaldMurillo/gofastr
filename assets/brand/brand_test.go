package brand

import (
	"bytes"
	"encoding/xml"
	"image/png"
	"strings"
	"testing"
)

func TestEmbeddedIdentity(t *testing.T) {
	var root struct {
		XMLName xml.Name
		Fill    string `xml:"fill,attr"`
		ViewBox string `xml:"viewBox,attr"`
	}
	if err := xml.Unmarshal([]byte(SVG()), &root); err != nil {
		t.Fatal(err)
	}
	if root.XMLName.Local != "svg" || root.Fill != "currentColor" || root.ViewBox != "-16 -16 317 213" {
		t.Fatalf("unexpected logo root: %+v", root)
	}
	for _, required := range []string{`id="symbol"`, `id="fastr"`, `aria-hidden="true"`} {
		if !strings.Contains(SVG(), required) {
			t.Errorf("logo missing %s", required)
		}
	}
	for _, forbidden := range []string{"<image", "<text", "http://www.w3.org/1999/xlink"} {
		if strings.Contains(SVG(), forbidden) {
			t.Errorf("logo must remain self-contained paths: %s", forbidden)
		}
	}
	icon, err := png.Decode(bytes.NewReader(IconPNG()))
	if err != nil {
		t.Fatal(err)
	}
	if icon.Bounds().Dx() != 512 || icon.Bounds().Dy() != 512 {
		t.Fatalf("unexpected icon bounds: %v", icon.Bounds())
	}
	first := IconPNG()
	first[0] = 0
	if bytes.Equal(first, IconPNG()) {
		t.Fatal("callers can mutate the shared app icon")
	}
}
