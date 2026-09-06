package main

import (
	"strings"
	"testing"
)

// Info.plist rendering for the deep-link scheme: --scheme emits
// CFBundleURLTypes claiming that one scheme; no scheme emits nothing.

func renderTestPlist(scheme string) string {
	return string(renderInfoPlist(infoPlistValues{
		Name:         "Notes",
		DisplayName:  "Notes",
		Identifier:   "dev.gofastr.notes",
		Executable:   "Notes",
		ShortVersion: "0.1.0",
		Version:      "0.1.0",
		Scheme:       scheme,
	}))
}

func TestInfoPlistEmitsURLTypesForScheme(t *testing.T) {
	plist := renderTestPlist("notes")
	for _, want := range []string{
		"<key>CFBundleURLTypes</key>",
		"<key>CFBundleURLName</key>",
		"<string>dev.gofastr.notes</string>",
		"<key>CFBundleURLSchemes</key>",
		"<string>notes</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Fatalf("plist with scheme \"notes\" is missing %q:\n%s", want, plist)
		}
	}
}

func TestInfoPlistOmitsURLTypesWithoutScheme(t *testing.T) {
	if plist := renderTestPlist(""); strings.Contains(plist, "CFBundleURLTypes") {
		t.Fatalf("plist without a scheme mentions CFBundleURLTypes:\n%s", plist)
	}
}

func TestInfoPlistSchemeIsXMLEscaped(t *testing.T) {
	// The renderer escapes every value it embeds; the scheme slot is
	// no exception even though the flag validation would refuse this.
	plist := renderTestPlist("a<b&c")
	if strings.Contains(plist, "<string>a<b&c</string>") {
		t.Fatalf("scheme embedded unescaped:\n%s", plist)
	}
	if !strings.Contains(plist, "<string>a&lt;b&amp;c</string>") {
		t.Fatalf("scheme not escaped as expected:\n%s", plist)
	}
}
