package uihost

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// manifestEntries parses the route array out of the manifest script body.
func manifestEntries(t *testing.T, ds *UIHost) []map[string]any {
	t.Helper()
	body := ds.buildRouteScript()
	start := strings.IndexByte(body, '[')
	end := strings.LastIndexByte(body, ']')
	if start < 0 || end <= start {
		t.Fatalf("no manifest JSON array in %q", body)
	}
	var raw []map[string]any
	if err := json.Unmarshal([]byte(body[start:end+1]), &raw); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	return raw
}

// A NoSPA screen is absent from the manifest, and its slot does not survive
// as a zero-value entry. infos was sized from the pre-filter route count, so
// every excluded route left a trailing {"path":""} the client parsed as a
// real route.
func TestNoSPARouteLeavesNoEmptyManifestEntry(t *testing.T) {
	a := app.NewApp("nospa")
	a.Register("/", &testHomeComp{}, nil)
	a.Register("/keep", &testHomeComp{}, nil)
	legacy := app.NewScreen("/legacy", &testHomeComp{})
	legacy.NoSPA = true
	a.RegisterScreen(legacy, nil)

	entries := manifestEntries(t, New(a))
	paths := make([]string, len(entries))
	for i, e := range entries {
		p, _ := e["path"].(string)
		if p == "" {
			t.Errorf("entry %d has an empty path: %v", i, e)
		}
		paths[i] = p
	}
	if len(entries) != 2 {
		t.Fatalf("manifest has %d entries %v, want 2 (/ and /keep)", len(entries), paths)
	}
	for _, p := range paths {
		if p == "/legacy" {
			t.Errorf("NoSPA route /legacy is in the manifest: %v", paths)
		}
	}
}

// Every route being NoSPA yields no manifest rather than an array of empty
// entries.
func TestAllRoutesNoSPAYieldsNoManifest(t *testing.T) {
	a := app.NewApp("nospa")
	only := app.NewScreen("/legacy", &testHomeComp{})
	only.NoSPA = true
	a.RegisterScreen(only, nil)

	if body := New(a).buildRouteScript(); strings.Contains(body, `"path":""`) {
		t.Errorf("manifest carries empty-path entries: %s", body)
	}
}
