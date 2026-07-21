package widget_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// TestBuilderPoll_RecordsMilliseconds verifies Builder.Poll converts a
// time.Duration to whole milliseconds on Definition.PollMS verbatim —
// the runtime applies no further clamping on the widget path.
func TestBuilderPoll_RecordsMilliseconds(t *testing.T) {
	def := widget.New("p").
		Poll(2500 * time.Millisecond).
		Build()
	if def.PollMS != 2500 {
		t.Errorf("PollMS = %d, want 2500", def.PollMS)
	}

	// Sub-millisecond truncation: 800µs rounds down to 0ms. The catalog
	// gate (PollMS > 0) then suppresses emission — the caller asked for
	// an impossibly fast poll and gets silence instead of a busy-loop.
	def = widget.New("p").Poll(800 * time.Microsecond).Build()
	if def.PollMS != 0 {
		t.Errorf("PollMS = %d, want 0 for sub-millisecond interval", def.PollMS)
	}
}

// TestCatalog_PollMsEmittedOnlyWithSignals locks the catalog gate:
// pollMs appears in /__gofastr/widgets cfg ONLY when the widget
// declares BOTH PollMS > 0 AND at least one Signal. The runtime
// requires a statePath to poll; a widget without signals has none.
func TestCatalog_PollMsEmittedOnlyWithSignals(t *testing.T) {
	cases := []struct {
		name      string
		hasSignal bool
		hasPoll   bool
		wantPoll  bool
	}{
		{"poll+signal → emit", true, true, true},
		{"poll, no signal → suppress", false, true, false},
		{"signal, no poll → suppress", true, false, false},
		{"neither → suppress", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := widget.New("catalog-" + strings.ReplaceAll(tc.name, " ", "-"))
			if tc.hasSignal {
				b = b.Signal("count", widget.SignalFunc(func() (any, error) { return 0, nil }))
			}
			if tc.hasPoll {
				b = b.Poll(5 * time.Second)
			}
			def := b.Build()

			r := router.New()
			widget.Mount(r, &def)
			widget.MountRuntime(r)
			srv := httptest.NewServer(r)
			t.Cleanup(srv.Close)

			resp, err := http.Get(srv.URL + "/__gofastr/widgets")
			if err != nil || resp.StatusCode != 200 {
				t.Fatalf("catalog status: err=%v code=%d", err, resp.StatusCode)
			}
			defer resp.Body.Close()
			var entries []map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
				t.Fatalf("decode: %v", err)
			}
			cfg := findCfg(t, entries, def.Name)
			_, present := cfg["pollMs"]
			if present != tc.wantPoll {
				t.Errorf("pollMs present=%v, want %v (cfg=%v)", present, tc.wantPoll, cfg)
			}
			if tc.wantPoll {
				if got := cfg["pollMs"]; got != float64(5000) {
					t.Errorf("pollMs = %v, want 5000", got)
				}
			}
		})
	}
}
