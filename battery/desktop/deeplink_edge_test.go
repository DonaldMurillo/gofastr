package desktop_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

// An escaped "?" or "#" in a link's path stays part of the path: the
// default mapping builds from the escaped path, not the decoded one.
func TestDeepLinkKeepsEscapedPath(t *testing.T) {
	app, d := uiApp(t, desktop.Config{
		Title:    "DL",
		DeepLink: &desktop.DeepLinkConfig{Scheme: "notes"},
	})
	h := desktoptest.Run(t, app, d)

	h.OpenURL("notes://notes/a%3Fb%23c?x=1")
	h.Wait("the deep-link navigation", func() bool { return len(h.Navigations()) == 1 })
	if got := h.Navigations()[0]; got != "/notes/a%3Fb%23c?x=1" {
		t.Fatalf("navigation = %q, want /notes/a%%3Fb%%23c?x=1", got)
	}
}

// A percent-encoded dot segment is a traversal too: a URL parser reads
// %2e%2e as "..". Escaped paths must not carry one past the grammar.
func TestDeepLinkRefusesEncodedDots(t *testing.T) {
	app, d := uiApp(t, desktop.Config{
		Title:    "DL",
		DeepLink: &desktop.DeepLinkConfig{Scheme: "notes"},
	})
	h := desktoptest.Run(t, app, d)

	h.OpenURL("notes://host/%2e%2e/etc")
	h.OpenURL("notes://host/.%2E/etc")
	h.OpenURL("notes://host/%2E/etc")
	h.OpenURL("notes://ok/1")
	h.Wait("the good link", func() bool { return len(h.Navigations()) == 1 })
	time.Sleep(50 * time.Millisecond)
	if got := h.Navigations(); len(got) != 1 || got[0] != "/ok/1" {
		t.Fatalf("navigations = %q, want [/ok/1] (encoded dot segments dropped)", got)
	}
}

// An app with no DeepLink config drops a link the shell delivers anyway
// (a forwarded second-instance argument) instead of panicking.
func TestDeepLinkWithNoConfigIsDropped(t *testing.T) {
	app, d := uiApp(t, desktop.Config{Title: "DL"})
	h := desktoptest.Run(t, app, d)

	h.OpenURL("notes://notes/1")
	time.Sleep(50 * time.Millisecond)
	if got := len(h.Navigations()); got != 0 {
		t.Fatalf("navigations = %d, want 0", got)
	}
}

// A capability may not take a name the runtime's desktop object already
// uses: the bridge assigns NS.desktop[name], and "call" would replace
// the transport every typed method goes through.
func TestCapabilityRefusesRuntimeNames(t *testing.T) {
	_, d := uiApp(t, desktop.Config{Title: "Names"})
	for _, name := range []string{"available", "call", "on", "off", "manifest"} {
		err := d.Register(desktop.Capability{
			Name:    name,
			Version: 1,
			Methods: []desktop.Method{{
				Name:    "ping",
				Handler: func(context.Context, json.RawMessage) (any, error) { return nil, nil },
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("Register(%q) = %v, want a reserved-name error", name, err)
		}
	}
	if err := d.Register(desktop.Capability{
		Name:    "calls",
		Version: 1,
		Methods: []desktop.Method{{
			Name:    "ping",
			Handler: func(context.Context, json.RawMessage) (any, error) { return nil, nil },
		}},
	}); err != nil {
		t.Fatalf("Register(calls): %v", err)
	}
}
