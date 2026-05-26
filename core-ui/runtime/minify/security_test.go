package minify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMinify_PreservesURLSchemeGuardRegex pins the most security-relevant
// regex in the runtime — the URL-scheme guard that strips leading
// whitespace and control bytes before checking startsWith("javascript:"),
// "vbscript:", "data:" etc. The character class contains LITERAL NUL and
// other control bytes (0x00–0x1F) inside the brackets; a minifier that
// silently stripped or escaped those would let `\x00javascript:` slip
// past the guard.
func TestMinify_PreservesURLSchemeGuardRegex(t *testing.T) {
	// Embedded literal control bytes — these go straight into the regex
	// character class in runtime.js. The minifier must preserve them.
	src := "x.replace(/^[\\s\x00-\x1f]+/, '').toLowerCase()"
	out := Minify(src)
	for _, b := range []byte{0x00, 0x1f} {
		if !strings.ContainsRune(out, rune(b)) {
			t.Errorf("SECURITY: [scheme-guard] minified output missing control byte 0x%02x: %q", b, out)
		}
	}
	if !strings.Contains(out, "/^[\\s\x00-\x1f]+/") {
		t.Errorf("SECURITY: [scheme-guard] minified regex body altered: %q", out)
	}
}

// TestMinify_PreservesSchemeStringLiterals locks in that the scheme
// names compared via startsWith survive intact. A minifier that
// case-folded, trimmed, or otherwise touched string literal payloads
// would silently break the guard ("Javascript:" passes a case-folded
// "javascript:" check; etc).
func TestMinify_PreservesSchemeStringLiterals(t *testing.T) {
	src := `
		if (v.startsWith('javascript:')) return true;
		if (v.startsWith('vbscript:')) return true;
		if (v.startsWith('data:')) return true;
	`
	out := Minify(src)
	for _, want := range []string{"'javascript:'", "'vbscript:'", "'data:'"} {
		if !strings.Contains(out, want) {
			t.Errorf("SECURITY: [scheme-guard] string literal %s missing from minified output: %q", want, out)
		}
	}
}

// TestMinify_DoesNotIntroduceScriptCloser asserts the minifier never
// emits a literal </script> sequence that wasn't already present in
// the input. The bundled runtime is served as an external <script
// src=…> so this is defense-in-depth — but a host that ever inlines
// the bundle would be one buggy minifier transform away from an HTML
// parser premature-close.
func TestMinify_DoesNotIntroduceScriptCloser(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	paths := []string{filepath.Join(root, "core-ui/runtime/runtime.js")}
	srcDir := filepath.Join(root, "core-ui/runtime/src")
	entries, _ := os.ReadDir(srcDir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".js" {
			paths = append(paths, filepath.Join(srcDir, e.Name()))
		}
	}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		// `</script>` substring count must not increase. We compare
		// case-insensitively because HTML parsers also do.
		srcCount := strings.Count(strings.ToLower(string(raw)), "</script>")
		out := Minify(string(raw))
		outCount := strings.Count(strings.ToLower(out), "</script>")
		if outCount > srcCount {
			t.Errorf("SECURITY: [script-closer] %s introduced </script> sequence (in=%d out=%d)", filepath.Base(p), srcCount, outCount)
		}
	}
}

// TestMinify_PreservesIsUnsafeSignalUrlIdentifier ensures the runtime's
// scheme-guard function name survives untouched. The identifier is
// referenced from string-bound signal hydration; renaming it would
// silently disable the guard at every call site.
func TestMinify_PreservesIsUnsafeSignalUrlIdentifier(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "core-ui/runtime/runtime.js"))
	if err != nil {
		t.Fatalf("read runtime.js: %v", err)
	}
	out := Minify(string(raw))
	for _, want := range []string{
		"_isUnsafeSignalUrl",
		"'javascript:'",
		"'vbscript:'",
		"'data:'",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("SECURITY: [scheme-guard] minified runtime.js missing %q — the URL-scheme guard may be broken", want)
		}
	}
}
