package config

import (
	"reflect"
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

func TestRepeatedBounds(t *testing.T) {
	for _, kind := range []struct {
		name                    string
		strictFirst, strictLast any
		below, min, max, above  string
		minBound, maxBound      string
	}{
		{
			name: "string",
			strictFirst: &struct {
				Limit string `validate:"min=2,max=3,min=1,max=4"`
			}{},
			strictLast: &struct {
				Limit string `validate:"min=1,max=4,min=2,max=3"`
			}{},
			below: "é", min: "éé", max: "ééé", above: "éééé",
			minBound: "2 characters", maxBound: "3 characters",
		},
		{
			name: "int",
			strictFirst: &struct {
				Limit int `validate:"min=2,max=3,min=1,max=4"`
			}{},
			strictLast: &struct {
				Limit int `validate:"min=1,max=4,min=2,max=3"`
			}{},
			below: "1", min: "2", max: "3", above: "4",
			minBound: "2", maxBound: "3",
		},
		{
			name: "duration",
			strictFirst: &struct {
				Limit time.Duration `validate:"min=2s,max=3s,min=1s,max=4s"`
			}{},
			strictLast: &struct {
				Limit time.Duration `validate:"min=1s,max=4s,min=2s,max=3s"`
			}{},
			below: "1s", min: "2s", max: "3s", above: "4s",
			minBound: "2s", maxBound: "3s",
		},
		{
			name: "float",
			strictFirst: &struct {
				Limit float64 `validate:"min=0.5,max=2.5,min=0,max=3"`
			}{},
			strictLast: &struct {
				Limit float64 `validate:"min=0,max=3,min=0.5,max=2.5"`
			}{},
			below: "0.25", min: "0.5", max: "2.5", above: "2.75",
			minBound: "0.5", maxBound: "2.5",
		},
	} {
		for _, order := range []struct {
			name string
			cfg  any
		}{
			{"strict-first", kind.strictFirst},
			{"strict-last", kind.strictLast},
		} {
			for _, tc := range []struct {
				name, value, want string
			}{
				{"below", kind.below, "must be at least " + kind.minBound},
				{"min", kind.min, ""},
				{"max", kind.max, ""},
				{"above", kind.above, "must be at most " + kind.maxBound},
			} {
				t.Run(kind.name+"/"+order.name+"/"+tc.name, func(t *testing.T) {
					err := Load(order.cfg, MapSource{"LIMIT": tc.value})
					if tc.want == "" {
						if err != nil {
							t.Fatalf("Load(%q) = %v, want success", tc.value, err)
						}
					} else if err == nil || !strings.Contains(err.Error(), tc.want) {
						t.Fatalf("Load(%q) = %v, want error containing %q", tc.value, err, tc.want)
					}
				})
			}
		}
	}
}

func TestContradictoryBounds(t *testing.T) {
	for _, kind := range []struct {
		name                         string
		typ                          reflect.Type
		low, max, min, high, atLimit string
	}{
		{"string", reflect.TypeFor[string](), "1", "2", "3", "4", "ééé"},
		{"int", reflect.TypeFor[int](), "1", "2", "3", "4", "3"},
		{"int64", reflect.TypeFor[int64](), "1", "9007199254740992", "9007199254740993", "9007199254740994", "9007199254740993"},
		{"duration", reflect.TypeFor[time.Duration](), "1s", "9007199254740992ns", "9007199254740993ns", "9007199254740994ns", "9007199254740993ns"},
		{"float", reflect.TypeFor[float64](), "0.5", "1.5", "2.5", "3.5", "2.5"},
	} {
		strict := "min=" + kind.min + ",max=" + kind.max
		loose := "min=" + kind.low + ",max=" + kind.high
		for _, tc := range []struct {
			name, tag string
			invalid   bool
		}{
			{"single", strict, true},
			{"strict-first", strict + "," + loose, true},
			{"strict-last", loose + "," + strict, true},
			{"equal", loose + ",min=" + kind.min + ",max=" + kind.min, false},
		} {
			t.Run(kind.name+"/"+tc.name, func(t *testing.T) {
				typ := reflect.StructOf([]reflect.StructField{{
					Name: "Limit",
					Type: kind.typ,
					Tag:  reflect.StructTag(`validate:"` + tc.tag + `"`),
				}})
				err := Load(reflect.New(typ).Interface(), MapSource{"LIMIT": kind.atLimit})
				if !tc.invalid {
					if err != nil {
						t.Fatalf("equal bounds refused: %v", err)
					}
					return
				}
				want := "config: field Limit: min=" + kind.min + " exceeds max=" + kind.max
				if err == nil || err.Error() != want {
					t.Fatalf("Load = %v, want %q", err, want)
				}
			})
		}
	}
	t.Run("unbound-nested", func(t *testing.T) {
		var cfg struct {
			Optional *struct {
				Limit int `validate:"min=3,max=2"`
			}
		}
		err := Load(&cfg, MapSource{})
		const want = "config: field Limit: min=3 exceeds max=2"
		if err == nil || err.Error() != want {
			t.Fatalf("Load = %v, want %q", err, want)
		}
	})
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
