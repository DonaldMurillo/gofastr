package settings

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const hintedEntry = refundEntry + `    hints:
      title: "Refund an order"
      destructive: true
      open_world: true
`

func TestEndpointHintsDecodeAndRoundTrip(t *testing.T) {
	got, err := parseEndpoints(t, hintedEntry)
	if err != nil {
		t.Fatal(err)
	}
	want := EndpointHints{Title: "Refund an order", Destructive: HintTrue, OpenWorld: HintTrue}
	if len(got) != 1 || got[0].Hints != want {
		t.Fatalf("hints = %+v, want %+v", got, want)
	}
	p := filepath.Join(t.TempDir(), FileName)
	writeFile(t, p, "version: 1\n")
	if err := SetEndpoints(p, got); err != nil {
		t.Fatalf("SetEndpoints: %v", err)
	}
	f, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if back, err := f.Endpoints(); err != nil || !slices.Equal(back, got) {
		t.Fatalf("read back %+v, err = %v", back, err)
	}
}

func TestEndpointHintsRefusals(t *testing.T) {
	for name, tc := range map[string]struct{ hints, want string }{
		"unknown key":  {"      read_ony: true\n", `unknown key "read_ony"`},
		"not a bool":   {"      read_only: yes please\n", "read_only must be true or false"},
		"title type":   {"      title: [a]\n", "title must be a string"},
		"long title":   {"      title: \"" + strings.Repeat("a", maxHintTitleBytes+1) + "\"\n", "at most 128 bytes"},
		"control char": {"      title: \"a\\u0007b\"\n", "one line of text"},
		"separator":    {"      title: \"a\\u2028b\"\n", "one line of text"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseEndpoints(t, refundEntry+"    hints:\n"+tc.hints)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to name %q", err, tc.want)
			}
		})
	}
	if _, err := parseEndpoints(t, refundEntry+"    hints: true\n"); err == nil || !strings.Contains(err.Error(), "hints must be a map") {
		t.Fatalf("scalar hints: err = %v", err)
	}
	noMCP := strings.Replace(hintedEntry, "    mcp: true\n", "", 1)
	if _, err := parseEndpoints(t, noMCP); err == nil || !strings.Contains(err.Error(), "set mcp: true") {
		t.Fatalf("hints without mcp: err = %v", err)
	}
	built := refundEndpoint()
	built.Hints.ReadOnly = Hint(7)
	if err := built.Validate(); err == nil {
		t.Fatal("Validate accepted a hint that is neither unset, false nor true")
	}
}

func TestEndpointMCPHintsDefaultByMethod(t *testing.T) {
	type hints struct{ readOnly, destructive, idempotent, openWorld bool }
	for method, want := range map[string]hints{
		"GET":    {true, false, true, false},
		"POST":   {false, false, false, false},
		"PUT":    {false, true, true, false},
		"PATCH":  {false, true, false, false},
		"DELETE": {false, true, true, false},
	} {
		h := Endpoint{Method: method}.MCPHints()
		if got := (hints{h.ReadOnly, h.Destructive, h.Idempotent, h.OpenWorld}); got != want {
			t.Errorf("%s defaults %+v, want %+v", method, got, want)
		}
	}
	override := Endpoint{Method: "DELETE", Hints: EndpointHints{Title: "Clear", Destructive: HintFalse, OpenWorld: HintTrue}}.MCPHints()
	if override.Destructive || !override.OpenWorld || !override.Idempotent || override.Title != "Clear" {
		t.Fatalf("an explicit hint did not win over the method default: %+v", override)
	}
}
