package config_test

import (
	"errors"
	"log/slog"
	"net/netip"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/config"
)

// tier is a validated enum: UnmarshalText admits only its members.
type tier string

func (t *tier) UnmarshalText(b []byte) error {
	switch s := string(b); s {
	case "free", "pro":
		*t = tier(s)
		return nil
	}
	return errors.New("unknown tier")
}

// A field whose type parses its own text binds through UnmarshalText, the
// rule core/handler's tag binder follows (4cd4d974). Before, slog.Level
// (an int) failed "debug" as "not a valid integer", a time.Time or
// netip.Addr field was taken for a nested struct and its key silently
// ignored, and a validated enum bound "bogus" raw through the string case.
func TestLoadBindsTextUnmarshaler(t *testing.T) {
	type cfg struct {
		Level   slog.Level  `config:"LEVEL"`
		Started time.Time   `config:"STARTED"`
		Addr    netip.Addr  `config:"ADDR"`
		Until   *time.Time  `config:"UNTIL"`
		Tier    tier        `config:"TIER"`
		Opt     *netip.Addr `config:"OPT"`
	}
	var c cfg
	err := config.Load(&c, config.MapSource{
		"LEVEL":   "debug",
		"STARTED": "2026-11-02T10:00:00Z",
		"ADDR":    "10.0.0.1",
		"UNTIL":   "2026-12-01T00:00:00Z",
		"TIER":    "pro",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Level != slog.LevelDebug {
		t.Errorf("Level = %v, want debug", c.Level)
	}
	if want := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC); !c.Started.Equal(want) {
		t.Errorf("Started = %v, want %v", c.Started, want)
	}
	if c.Addr != netip.MustParseAddr("10.0.0.1") {
		t.Errorf("Addr = %v", c.Addr)
	}
	if c.Until == nil || !c.Until.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("Until = %v", c.Until)
	}
	if c.Tier != "pro" {
		t.Errorf("Tier = %q", c.Tier)
	}
	if c.Opt != nil {
		t.Errorf("Opt = %v, want nil with no OPT key", c.Opt)
	}

	var bad cfg
	if err := config.Load(&bad, config.MapSource{"TIER": "bogus"}); err == nil {
		t.Fatalf("Load bound TIER=bogus; the enum's UnmarshalText refuses it")
	}
}
