package runtime

import (
	"os"
	"strings"
	"testing"
)

// The color-scheme bootstrap is the one script that blocks first paint,
// so it ships minified like every runtime module: its comments are
// source documentation, not bytes a visitor waits on.
func TestColorSchemeJSShipsMinified(t *testing.T) {
	if nominify() {
		t.Skip("minification disabled by the environment")
	}
	raw, err := os.ReadFile("colorscheme.js")
	if err != nil {
		t.Fatal(err)
	}
	js, err := ColorSchemeJS()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(js, "// GoFastr color-scheme") {
		t.Error("served bootstrap still carries its source comments")
	}
	if len(js) >= len(raw)*2/3 {
		t.Errorf("served bootstrap is %d bytes of %d source bytes; want it minified", len(js), len(raw))
	}
	if !strings.Contains(js, "gofastr.colorScheme") {
		t.Error("minified bootstrap lost its storage key")
	}
}
