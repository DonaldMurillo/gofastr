package ui

import (
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestPaneHostDeepLinkMarker(t *testing.T) {
	h := PaneHost(PaneHostConfig{
		Primary:       render.Text("P"),
		Secondary:     render.Text("S"),
		DeepLinkParam: "pane",
	})
	mustContain(t, h, `data-fui-pane-deeplink="pane"`)
}

// Opt-in: a host that does not ask for URL round-tripping must not carry
// the marker, or the runtime would start rewriting URLs under every
// existing PaneHost.
func TestPaneHostNoDeepLinkMarkerByDefault(t *testing.T) {
	h := string(PaneHost(PaneHostConfig{
		Primary:   render.Text("P"),
		Secondary: render.Text("S"),
	}))
	if strings.Contains(h, "data-fui-pane-deeplink") {
		t.Fatalf("marker emitted without DeepLinkParam:\n%s", h)
	}
}

func TestPaneDeepLinkParse(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		param     string
		slot, key string
		ok        bool
	}{
		{name: "slot and key", query: "pane=secondary:4021", param: "pane", slot: "secondary", key: "4021", ok: true},
		{name: "tertiary", query: "pane=tertiary:north", param: "pane", slot: "tertiary", key: "north", ok: true},
		{name: "slot only", query: "pane=secondary", param: "pane", slot: "secondary", key: "", ok: true},
		{name: "absent", query: "", param: "pane", ok: false},
		{name: "empty param name", query: "pane=secondary:4021", param: "", ok: false},
		{name: "unknown slot rejected", query: "pane=sidebar:4021", param: "pane", ok: false},
		{name: "primary rejected", query: "pane=primary:4021", param: "pane", ok: false},
		{name: "empty value", query: "pane=", param: "pane", ok: false},
		// A key may itself contain a colon; only the first splits.
		{name: "colon in key", query: "pane=secondary:a:b", param: "pane", slot: "secondary", key: "a:b", ok: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := url.ParseQuery(tc.query)
			if err != nil {
				t.Fatalf("bad query fixture: %v", err)
			}
			slot, key, ok := PaneDeepLink(q, tc.param)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (slot=%q key=%q)", ok, tc.ok, slot, key)
			}
			if !ok {
				return
			}
			if slot != tc.slot || key != tc.key {
				t.Fatalf("got (%q, %q), want (%q, %q)", slot, key, tc.slot, tc.key)
			}
		})
	}
}

// The parsed slot feeds SecondaryOpen/TertiaryOpen, so a caller can wire
// first paint straight from the URL.
func TestPaneDeepLinkDrivesSSROpen(t *testing.T) {
	q, _ := url.ParseQuery("pane=tertiary:north")
	slot, _, ok := PaneDeepLink(q, "pane")
	if !ok {
		t.Fatal("expected a parse")
	}
	h := PaneHost(PaneHostConfig{
		Primary:       render.Text("P"),
		Secondary:     render.Text("S"),
		Tertiary:      render.Text("T"),
		DeepLinkParam: "pane",
		SecondaryOpen: slot == "secondary",
		TertiaryOpen:  slot == "tertiary",
	})
	mustContain(t, h, "ui-pane-host--tertiary-open")
	if strings.Contains(string(h), "ui-pane-host--secondary-open") {
		t.Fatalf("secondary should stay closed:\n%s", h)
	}
}
