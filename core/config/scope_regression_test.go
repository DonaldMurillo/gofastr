package config

import (
	"testing"
	"time"
)

// An empty value (`KEY=` in a .env, a ConfigMap key with no value)
// leaves the field as the host set it and does not fail Load.
func TestEmptyValueKeepsHostValue(t *testing.T) {
	c := struct {
		Token   string
		Port    int
		Debug   bool
		Timeout time.Duration
	}{Token: "kept", Port: 8080, Debug: true, Timeout: time.Second}
	src := MapSource{"TOKEN": "", "PORT": "", "DEBUG": "", "TIMEOUT": ""}
	if err := Load(&c, src); err != nil {
		t.Fatal(err)
	}
	if c.Token != "kept" || c.Port != 8080 || !c.Debug || c.Timeout != time.Second {
		t.Fatalf("empty values changed the config: %+v", c)
	}
}

// The tags still check a field an empty value left alone: `LOG_LEVEL=`
// holds "", which is not one of the choices.
func TestEmptyValueStillValidated(t *testing.T) {
	c := struct {
		LogLevel string `config:"LOG_LEVEL" default:"info" validate:"oneof=debug info"`
	}{}
	if Load(&c, MapSource{"LOG_LEVEL": ""}) == nil {
		t.Fatal("empty LOG_LEVEL passed oneof")
	}
}

func TestDocumentedValidationTags(t *testing.T) {
	for _, src := range []MapSource{{"PORT": "0", "MODE": "info"}, {"PORT": "80", "MODE": "bad"}} {
		c := struct {
			Port int    `validate:"min=1,max=65535"`
			Mode string `validate:"oneof=info debug"`
		}{}
		if Load(&c, src) == nil {
			t.Fatalf("accepted %v", src)
		}
	}
}

func TestValidationTagsCheckUnboundDefaults(t *testing.T) {
	c := struct {
		Port int `validate:"min=1"`
	}{}
	if e := Load(&c, MapSource{}); e == nil {
		t.Fatal("unset port zero bypassed minimum")
	}
	c.Port = 80
	if e := Load(&c, MapSource{}); e != nil {
		t.Fatal(e)
	}
}

func TestDurationBoundsReadAsDurations(t *testing.T) {
	type cfg struct {
		Timeout time.Duration `validate:"min=1s,max=5m"`
	}
	var ok cfg
	if err := Load(&ok, MapSource{"TIMEOUT": "30s"}); err != nil {
		t.Fatalf("30s inside [1s, 5m]: %v", err)
	}
	for _, v := range []string{"10m", "500ms"} {
		var c cfg
		if Load(&c, MapSource{"TIMEOUT": v}) == nil {
			t.Fatalf("%s outside [1s, 5m] accepted", v)
		}
	}
}

// A rule Load cannot apply is refused, never skipped: a skipped rule is a
// bound the host believes holds.
func TestUnappliableValidationRulesRefused(t *testing.T) {
	cases := []any{
		&struct {
			URL string `validate:"required,url"`
		}{URL: "x"},
		&struct {
			On bool `validate:"min=1"`
		}{},
		&struct {
			N int `validate:"oneof=1 2"`
		}{N: 1},
	}
	for _, c := range cases {
		if Load(c, MapSource{}) == nil {
			t.Fatalf("%T: unappliable rule silently skipped", c)
		}
	}
}
