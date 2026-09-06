package desktop

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// Group 9: the generated bridge script and the DTS.

func testManifest(t *testing.T) Manifest {
	t.Helper()
	b, _ := newTestBattery(t)
	err := b.Register(Capability{
		Name:        "systeminfo",
		Description: "Host information.",
		Version:     2,
		Methods: []Method{
			{
				Name:        "cpuCount",
				Description: "Number of CPUs.",
				Output:      json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer"}}}`),
				Handler:     func(ctx context.Context, in json.RawMessage) (any, error) { return nil, nil },
			},
			{
				Name:        "env",
				Description: "Environment name.",
				Input:       json.RawMessage(`{"type":"object","properties":{"verbose":{"type":"boolean"}},"required":["verbose"]}`),
				Handler:     func(ctx context.Context, in json.RawMessage) (any, error) { return nil, nil },
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	b.freezeForTest()
	m, err := b.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestBridgeJSBracketStringAccessOnly(t *testing.T) {
	m := testManifest(t)
	js := string(BridgeJS(m))

	// No bare-identifier namespace assignment other than D.manifest:
	// every name must land through a quoted string.
	bare := regexp.MustCompile(`D\.[a-z]+\s*=`)
	if loc := bare.FindAllString(js, -1); loc != nil {
		for _, l := range loc {
			if l != "D.manifest =" && !strings.HasPrefix(l, "D.manifest=") {
				t.Fatalf("bare identifier assignment %q in generated bridge:\n%s", l, js)
			}
		}
	}
	// The typed namespaces use bracket keys.
	for _, want := range []string{
		`D["systeminfo"]`,
		`"cpuCount": (input) => D.call("systeminfo", "cpuCount", input)`,
		`D["clipboard"]`,
		`D["fs"]`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("generated bridge missing %q:\n%s", want, js)
		}
	}
	// The manifest is one quoted JSON string handed to JSON.parse.
	if !strings.Contains(js, `D.manifest = JSON.parse("`) {
		t.Fatalf("manifest not a JSON.parse string argument:\n%s", js[:min(300, len(js))])
	}
	// The manifest round-trips: extract the quoted string and parse it.
	i := strings.Index(js, `JSON.parse("`)
	rest := js[i+len(`JSON.parse("`):]
	j := strings.Index(rest, `")`)
	if j < 0 {
		t.Fatal("cannot find the end of the manifest string")
	}
	var parsed Manifest
	if err := json.Unmarshal([]byte(unescapeGo(rest[:j])), &parsed); err != nil {
		t.Fatalf("embedded manifest does not parse: %v", err)
	}
	if len(parsed.Capabilities) != len(m.Capabilities) {
		t.Fatalf("embedded manifest capability count = %d, want %d", len(parsed.Capabilities), len(m.Capabilities))
	}

	// Structural syntax: IIFE (after the leading generated-by comment),
	// balanced delimiters.
	trimmed := strings.TrimSpace(js)
	for strings.HasPrefix(trimmed, "//") {
		nl := strings.Index(trimmed, "\n")
		if nl < 0 {
			break
		}
		trimmed = strings.TrimSpace(trimmed[nl+1:])
	}
	if !strings.HasPrefix(trimmed, "(() => {") || !strings.HasSuffix(strings.TrimSpace(js), "})();") {
		t.Fatalf("bridge.js is not an IIFE: %q…", trimmed[:min(60, len(trimmed))])
	}
	for _, pair := range [][2]string{{"{", "}"}, {"(", ")"}, {"[", "]"}} {
		if strings.Count(js, pair[0]) != strings.Count(js, pair[1]) {
			t.Errorf("unbalanced %q in bridge.js", pair[0])
		}
	}
}

// unescapeGo reverses the minimal quoting strconv.Quote produces for
// our manifest bytes (quotes and backslashes).
func unescapeGo(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'u':
				// \uXXXX
				if i+4 < len(s) {
					var r rune
					for k := 1; k <= 4; k++ {
						c := s[i+k]
						r = r*16 + rune(hexVal(c))
					}
					b.WriteRune(r)
					i += 4
				}
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return 0
}

func TestBridgeJSNamesRefusedByRegistryNeverEmit(t *testing.T) {
	// A capability with an invalid name cannot be registered at all, so
	// the generator never sees one; the registry guard is proven in
	// capability_test.go. Here: assert the emitter only ever sees
	// validated names by construction (register panics otherwise).
	b, _ := newTestBattery(t)
	func() {
		defer func() { _ = recover() }()
		_ = b.Register(Capability{Name: "Bad Name", Version: 1,
			Methods: []Method{{Name: "x", Handler: func(ctx context.Context, in json.RawMessage) (any, error) { return nil, nil }}}})
	}()
	b.freezeForTest()
	m, err := b.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	js := string(BridgeJS(m))
	if strings.Contains(js, "Bad Name") {
		t.Fatal("unvalidated name reached the generated script")
	}
}

func TestDTSNamespacesPerCapability(t *testing.T) {
	m := testManifest(t)
	dts := DTS(m)
	for _, want := range []string{
		"declare global",
		"interface Window",
		`"systeminfo": {`,
		"// v2",
		`"cpuCount": (): Promise<{ "count"?: number }>;`,
		`"env": (input: { "verbose": boolean }): Promise<void>;`,
		`"clipboard": {`,
		`"fs": {`,
	} {
		if !strings.Contains(dts, want) {
			t.Fatalf("DTS missing %q:\n%s", want, dts)
		}
	}
}
