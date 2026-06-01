package runtime

import (
	"os"
	"testing"
	"unicode/utf8"
)

// TestRuntimeJSIsCleanText guards against control-byte corruption in
// the embedded runtime source. A literal NUL (or other C0 control
// byte) crept in once when an editor collapsed a `\x00` regex escape
// into the raw byte — that makes the file "binary" to grep/diff tools
// and risks minifier/embed edge cases. The served payload must stay
// valid UTF-8 with no stray control bytes (tab/newline/CR excepted).
func TestRuntimeJSIsCleanText(t *testing.T) {
	for _, name := range []string{"runtime.js", "colorscheme.js"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !utf8.Valid(raw) {
			t.Errorf("%s is not valid UTF-8", name)
		}
		for i, b := range raw {
			if b == '\t' || b == '\n' || b == '\r' {
				continue
			}
			if b < 0x20 || b == 0x7f {
				t.Errorf("%s: control byte 0x%02x at offset %d", name, b, i)
			}
		}
	}
}

// TestModuleSourcesAreCleanText extends the same guarantee to every
// demand-loaded module under src/.
func TestModuleSourcesAreCleanText(t *testing.T) {
	entries, err := os.ReadDir("src")
	if err != nil {
		t.Fatalf("read src dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile("src/" + e.Name())
		if err != nil {
			t.Fatalf("read src/%s: %v", e.Name(), err)
		}
		if !utf8.Valid(raw) {
			t.Errorf("src/%s is not valid UTF-8", e.Name())
		}
		for i, b := range raw {
			if b == '\t' || b == '\n' || b == '\r' {
				continue
			}
			if b < 0x20 || b == 0x7f {
				t.Errorf("src/%s: control byte 0x%02x at offset %d", e.Name(), b, i)
			}
		}
	}
}
