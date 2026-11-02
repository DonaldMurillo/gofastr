package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// shadowBase is a reusable config block whose methods read its own Port.
type shadowBase struct {
	Port int `config:"PORT" default:"8080"`
	DB   shadowDB
}

func (b shadowBase) addr() int { return b.Port }

type shadowDB struct {
	Host string `config:"HOST" default:"localhost"`
}

// A field shadowed by an outer one with the same key is never bound from
// a source, so it takes its own default tag. It used to stay zero, so a
// method on the embedded type reading its own Port read 0 where, before
// promotion, it read BASE_PORT or its 8080 default. A shadowed named
// struct takes its fields' defaults the same way.
func TestShadowedFieldTakesItsDefault(t *testing.T) {
	type cfg struct {
		shadowBase
		Port int `config:"PORT"`
		DB   shadowDB
	}
	var c cfg
	if err := Load(&c, MapSource{"PORT": "9", "DB_HOST": "db"}); err != nil {
		t.Fatal(err)
	}
	if c.Port != 9 {
		t.Errorf("outer Port = %d, want 9 from PORT", c.Port)
	}
	if got := c.shadowBase.addr(); got != 8080 {
		t.Errorf("shadowed Base.Port = %d, want its 8080 default", got)
	}
	if c.DB.Host != "db" {
		t.Errorf("outer DB.Host = %q, want db", c.DB.Host)
	}
	if c.shadowBase.DB.Host != "localhost" {
		t.Errorf("shadowed Base.DB.Host = %q, want its localhost default", c.shadowBase.DB.Host)
	}
}

// A shadowed field with a default that does not parse fails Load, as the
// same default on a bound field does.
func TestShadowedFieldBadDefaultFails(t *testing.T) {
	type Inner struct {
		Port int `config:"PORT" default:"eighty"`
	}
	type cfg struct {
		Inner
		Port int `config:"PORT"`
	}
	var c cfg
	if err := Load(&c, MapSource{"PORT": "1"}); err == nil || !strings.Contains(err.Error(), "eighty") {
		t.Fatalf("Load = %v, want the shadowed field's bad default refused", err)
	}
}

// captureWarnings routes slog's default logger into a buffer for the test
// and forgets which legacy keys were already reported.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	legacyWarned.Clear()
	t.Cleanup(func() {
		slog.SetDefault(prev)
		legacyWarned.Clear()
	})
	return &buf
}

type Legacy struct {
	Port int `config:"PORT"`
	DB   shadowDB
}

// A key spelled the way Load read a promoted field before promotion
// (<EMBED>_<KEY>) is no longer read. When a source still holds one, Load
// logs a warning naming the key it reads now, once per key per process,
// and does not fail.
func TestLegacyEmbedKeyWarns(t *testing.T) {
	buf := captureWarnings(t)
	type cfg struct{ Legacy }
	var c cfg
	src := MapSource{"LEGACY_PORT": "1", "LEGACY_DB_HOST": "h"}
	for range 2 {
		if err := Load(&c, src); err != nil {
			t.Fatal(err)
		}
	}
	out := buf.String()
	for _, want := range []string{"key=LEGACY_PORT", "reads=PORT", "key=LEGACY_DB_HOST", "reads=DB_HOST"} {
		if !strings.Contains(out, want) {
			t.Errorf("warnings %q lack %q", out, want)
		}
	}
	if n := strings.Count(out, "key=LEGACY_PORT"); n != 1 {
		t.Errorf("LEGACY_PORT warned %d times over two loads, want once", n)
	}
	if c.Port != 0 {
		t.Errorf("Port = %d; the legacy key must stay unread", c.Port)
	}
}

// A chain of embeds was a chain of prefixes: Outer{Mid}, Mid{Legacy}
// read MID_LEGACY_PORT.
func TestLegacyKeyThroughEmbedChainWarns(t *testing.T) {
	buf := captureWarnings(t)
	type Mid struct{ Legacy }
	type cfg struct{ Mid }
	var c cfg
	if err := Load(&c, MapSource{"MID_LEGACY_PORT": "1"}); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); !strings.Contains(out, "key=MID_LEGACY_PORT") || !strings.Contains(out, "reads=PORT") {
		t.Errorf("warnings = %q, want MID_LEGACY_PORT reported with PORT", out)
	}
}

// No warning when the old spelling is a key Load does read (an outer
// field named it), when nothing holds it, or for an unexported embed,
// whose fields Load never read before promotion.
func TestLegacyEmbedKeyQuietCases(t *testing.T) {
	buf := captureWarnings(t)
	type outer struct {
		Legacy
		Old int `config:"LEGACY_PORT"`
	}
	var o outer
	if err := Load(&o, MapSource{"LEGACY_PORT": "3"}); err != nil {
		t.Fatal(err)
	}
	if o.Old != 3 {
		t.Fatalf("Old = %d, want 3", o.Old)
	}
	type quiet struct{ Legacy }
	var q quiet
	if err := Load(&q, MapSource{"PORT": "4"}); err != nil {
		t.Fatal(err)
	}
	type hidden struct{ shadowDB }
	var h hidden
	if err := Load(&h, MapSource{"SHADOWDB_HOST": "x"}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("unexpected warnings: %s", buf.String())
	}
}

// A shadowed field's old key is reported too: it is not read, and the
// field takes its default.
func TestLegacyKeyOfShadowedFieldWarns(t *testing.T) {
	buf := captureWarnings(t)
	type cfg struct {
		Legacy
		Port int `config:"PORT"`
	}
	var c cfg
	if err := Load(&c, MapSource{"LEGACY_PORT": "5"}); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); !strings.Contains(out, "key=LEGACY_PORT") || !strings.Contains(out, "shadowed") {
		t.Errorf("warnings = %q, want LEGACY_PORT reported as shadowed", out)
	}
}

// The outer field's key allocates nothing on a shadowed path: a nil
// pointer embed whose only field is shadowed stays nil when PORT is set,
// as it did when shadowed fields were left out of the walk.
func TestShadowedPointerEmbedStaysNil(t *testing.T) {
	type Inner struct {
		Port int `config:"PORT" default:"80"`
	}
	type cfg struct {
		*Inner
		Port int `config:"PORT"`
	}
	var c cfg
	if err := Load(&c, MapSource{"PORT": "1"}); err != nil {
		t.Fatal(err)
	}
	if c.Inner != nil || c.Port != 1 {
		t.Fatalf("Inner = %+v, Port = %d; want a nil embed and Port 1", c.Inner, c.Port)
	}
}
