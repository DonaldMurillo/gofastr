package config

import (
	"strings"
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

func TestRepeatedFloatBounds(t *testing.T) {
	for _, order := range []struct {
		name string
		cfg  any
	}{
		{"strict-first", &struct {
			Limit float64 `validate:"min=0.5,max=2.5,min=0,max=3"`
		}{}},
		{"strict-last", &struct {
			Limit float64 `validate:"min=0,max=3,min=0.5,max=2.5"`
		}{}},
	} {
		for _, tc := range []struct {
			value string
			want  string
		}{
			{"0.25", "must be at least 0.5"},
			{"0.5", ""},
			{"2.5", ""},
			{"2.75", "must be at most 2.5"},
		} {
			t.Run(order.name+"/"+tc.value, func(t *testing.T) {
				err := Load(order.cfg, MapSource{"LIMIT": tc.value})
				if tc.want == "" {
					if err != nil {
						t.Fatalf("Load() = %v, want success", err)
					}
				} else if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("Load() = %v, want error containing %q", err, tc.want)
				}
			})
		}
	}
}

func TestRepeatedStringBoundsKeepLast(t *testing.T) {
	var c struct {
		Name string `validate:"min=4,max=2,min=1,max=5"`
	}
	if err := Load(&c, MapSource{"NAME": "abc"}); err != nil {
		t.Fatalf("Load() = %v, want success under last string bounds", err)
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

// Integer and duration bounds compare exactly: past 2^53 a float64 bound
// rounds, and a value one below the declared minimum would pass.
func TestIntegerBoundsCompareExactly(t *testing.T) {
	d := struct {
		Wait time.Duration `validate:"min=9007199254740993ns"`
	}{}
	if Load(&d, MapSource{"WAIT": "9007199254740992ns"}) == nil {
		t.Fatal("duration below min accepted")
	}
	n := struct {
		N int64 `validate:"max=9007199254740992"`
	}{}
	if Load(&n, MapSource{"N": "9007199254740993"}) == nil {
		t.Fatal("int above max accepted")
	}
	if err := Load(&n, MapSource{"N": "9007199254740992"}); err != nil {
		t.Fatalf("int at max refused: %v", err)
	}
}
