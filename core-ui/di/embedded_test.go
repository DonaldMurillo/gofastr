package di

import (
	"reflect"
	"testing"
)

type baseServices struct {
	Log *Logger `inject:""`
}

type SharedServices struct {
	DB *Database `inject:""`
}

type pageWithEmbeds struct {
	baseServices
	*SharedServices
	Cfg *Config `inject:""`
}

// Inject fills fields promoted from an embedded struct, value or
// pointer, the way encoding/json promotes them.
func TestInjectFillsEmbeddedFields(t *testing.T) {
	c := NewContainer()
	c.Provide(&Logger{Prefix: "base"})
	c.Provide(&Database{DSN: "db"})
	c.Provide(&Config{Env: "test"})

	p := &pageWithEmbeds{}
	if err := c.Inject(p); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if p.Log == nil || p.Log.Prefix != "base" {
		t.Errorf("Log from the embedded value struct = %v, want injected", p.Log)
	}
	if p.SharedServices == nil || p.DB == nil || p.DB.DSN != "db" {
		t.Errorf("DB from the embedded pointer struct = %+v, want allocated and injected", p.SharedServices)
	}
	if p.Cfg == nil {
		t.Error("Cfg should be injected")
	}
}

type hiddenServices struct {
	Log *Logger `inject:""`
}

type pageWithHiddenPtr struct {
	*hiddenServices
}

// A nil unexported embedded pointer cannot be allocated, so its inject
// tags would stay empty: that is a wiring error, not a silent nil.
func TestInjectRefusesNilHiddenEmbed(t *testing.T) {
	c := NewContainer()
	c.Provide(&Logger{})
	if err := c.Inject(&pageWithHiddenPtr{}); err == nil {
		t.Fatal("Inject left a nil unexported embedded pointer's inject field empty without an error")
	}
}

type selfEmbed struct {
	*selfEmbed
	Log *Logger `inject:""`
}

// A struct that embeds a pointer to itself counts once and injects.
func TestInjectSelfEmbedTerminates(t *testing.T) {
	if n := InjectTagCount(reflect.TypeOf(selfEmbed{})); n != 1 {
		t.Fatalf("InjectTagCount = %d, want 1", n)
	}
	c := NewContainer()
	c.Provide(&Logger{})
	s := &selfEmbed{}
	if err := c.Inject(s); err != nil || s.Log == nil {
		t.Fatalf("Inject = %v, Log = %v", err, s.Log)
	}
}

// The count that refuses a value-registered component sees promoted
// inject tags too.
func TestInjectTagCountSeesEmbeds(t *testing.T) {
	if n := InjectTagCount(reflect.TypeOf(pageWithEmbeds{})); n != 3 {
		t.Fatalf("InjectTagCount = %d, want 3", n)
	}
}
