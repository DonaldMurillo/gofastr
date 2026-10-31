package widget_test

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// getState fetches the widget's /state and reports the stop header and
// the decoded body.
func getState(t *testing.T, def *widget.Definition) (bool, map[string]any) {
	t.Helper()
	r := router.New()
	widget.Mount(r, def)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + def.StatePath)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("state: err=%v", err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return resp.Header.Get("X-Gofastr-Poll-Stop") != "", body
}

// A job that finished between the signal read and the PollTerminal call
// stopped the poll on a snapshot still saying "running" (2026-10-27 R12).
// Terminal is decided before the read, so the stopping snapshot is at
// least as new as the decision.
func TestPollStopNeverOnStaleSnapshot(t *testing.T) {
	var done atomic.Bool
	def := widget.New("pollstop-stale").
		Signal("status", widget.SignalFunc(func() (any, error) {
			if done.Load() {
				return "completed", nil
			}
			// The job completes right after this read.
			defer done.Store(true)
			return "running", nil
		})).
		PollTerminal(done.Load).
		Poll(time.Second).
		Build()
	stop, body := getState(t, &def)
	if stop && body["status"] != "completed" {
		t.Fatalf("stop sent with status %v", body["status"])
	}
}

// A signal whose Read failed was dropped from the body, and the stop
// header still went out: the runtime ended the cadence on a snapshot
// missing that value, for good (2026-10-27 R12).
func TestPollStopNeverOnPartialRead(t *testing.T) {
	for name, src := range map[string]widget.SignalFunc{
		"read fails": func() (any, error) { return nil, errors.New("db down") },
		"no JSON":    func() (any, error) { return math.NaN(), nil },
	} {
		t.Run(name, func(t *testing.T) {
			def := widget.New("pollstop-partial-"+strings.ReplaceAll(name, " ", "-")).
				Signal("status", widget.SignalFunc(func() (any, error) { return "completed", nil })).
				Signal("total", src).
				PollTerminal(func() bool { return true }).
				Poll(time.Second).
				Build()
			stop, body := getState(t, &def)
			if stop {
				t.Fatalf("stop sent on a partial snapshot %v", body)
			}
		})
	}
}
