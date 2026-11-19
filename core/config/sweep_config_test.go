package config

import (
	"strings"
	"testing"
)

func TestSweepConfigEnvBool(t *testing.T) {
	for val, want := range map[string]bool{"1": true, "true": true, "TRUE": true, "t": true, "0": false, "false": false, "yes": false, "on": false, "": false} {
		t.Setenv("GOFASTR_SWEEP_ENVBOOL", val)
		if got := EnvBool("GOFASTR_SWEEP_ENVBOOL"); got != want {
			t.Errorf("EnvBool(%q) = %v, want %v", val, got, want)
		}
	}
	if EnvBool("GOFASTR_SWEEP_ENVBOOL_UNSET") {
		t.Error("an unset variable reads true")
	}
}

func TestSweepConfigDefaultSatisfiesRequired(t *testing.T) {
	var cfg struct {
		Port int `config:"PORT" default:"8080" required:"true"`
	}
	if err := Load(&cfg, MapSource{}); err != nil || cfg.Port != 8080 {
		t.Fatalf("Load = %v, Port = %d; a default satisfies required when the key is absent", err, cfg.Port)
	}
}

func TestSweepConfigSensitiveBadDefaultRedacted(t *testing.T) {
	type base struct {
		Token int `config:"TOKEN" default:"s3cret-not-an-int" sensitive:"true"`
	}
	var cfg struct {
		base
		Token string `config:"TOKEN"`
	}
	err := Load(&cfg, MapSource{"TOKEN": "x"})
	if err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("Load = %v, want an error that does not quote the sensitive default", err)
	}
}

func TestSweepConfigEmptyValueSkipsDefault(t *testing.T) {
	cfg := struct {
		Level string `config:"LEVEL" default:"info"`
	}{Level: "set-by-host"}
	if err := Load(&cfg, MapSource{"LEVEL": ""}); err != nil || cfg.Level != "set-by-host" {
		t.Fatalf("Load = %v, Level = %q; an empty value keeps the host's value, not the default", err, cfg.Level)
	}
}

func TestSweepConfigUnknownRuleRefused(t *testing.T) {
	var cfg struct {
		Name string `config:"NAME" validate:"regex=^a"`
	}
	if err := Load(&cfg, MapSource{"NAME": "abc"}); err == nil {
		t.Fatal("an unknown validate rule was ignored")
	}
}

func TestSweepConfigPointerToNonStructRefused(t *testing.T) {
	n := 0
	if err := Load(&n, MapSource{}); err == nil {
		t.Fatal("Load accepted a pointer to an int")
	}
}

func TestSweepConfigUntaggedFieldReadsUpperName(t *testing.T) {
	var cfg struct{ Region string }
	if err := Load(&cfg, MapSource{"REGION": "eu", "Region": "us"}); err != nil || cfg.Region != "eu" {
		t.Fatalf("Load = %v, Region = %q; an untagged field reads its upper-cased name", err, cfg.Region)
	}
}

type sweepDB struct {
	Host string `config:"HOST" default:"localhost"`
}

type sweepBase struct {
	DB *sweepDB
}

func TestSweepConfigShadowedNestedPointerStaysNil(t *testing.T) {
	var cfg struct {
		sweepBase
		DB sweepDB
	}
	if err := Load(&cfg, MapSource{"DB_HOST": "db.internal"}); err != nil {
		t.Fatal(err)
	}
	if cfg.DB.Host != "db.internal" || cfg.sweepBase.DB != nil {
		t.Fatalf("outer DB = %+v, shadowed DB = %v; the shadowed pointer must stay nil", cfg.DB, cfg.sweepBase.DB)
	}
}
