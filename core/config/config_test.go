package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/config"
)

type testConfig struct {
	Port     int           `config:"PORT" default:"8080"`
	DBURL    string        `config:"DATABASE_URL" required:"true"`
	Debug    bool          `config:"DEBUG" default:"false"`
	LogLevel string        `config:"LOG_LEVEL" default:"info"`
	Timeout  time.Duration `config:"TIMEOUT" default:"30s"`
	Rate     float64       `config:"RATE" default:"1.5"`
}

func TestLoadFromMap(t *testing.T) {
	src := config.MapSource{
		"PORT":         "3000",
		"DATABASE_URL": "postgres://localhost/test",
		"DEBUG":        "true",
		"LOG_LEVEL":    "debug",
		"TIMEOUT":      "60s",
		"RATE":         "2.5",
	}

	var cfg testConfig
	if err := config.Load(&cfg, src); err != nil {
		t.Fatal(err)
	}

	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want 3000", cfg.Port)
	}
	if cfg.DBURL != "postgres://localhost/test" {
		t.Errorf("DBURL = %q, want %q", cfg.DBURL, "postgres://localhost/test")
	}
	if !cfg.Debug {
		t.Error("Debug = false, want true")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if cfg.Timeout != 60*time.Second {
		t.Errorf("Timeout = %v, want 60s", cfg.Timeout)
	}
	if cfg.Rate != 2.5 {
		t.Errorf("Rate = %f, want 2.5", cfg.Rate)
	}
}

func TestLoadDefaults(t *testing.T) {
	src := config.MapSource{
		"DATABASE_URL": "postgres://localhost/test",
	}

	var cfg testConfig
	if err := config.Load(&cfg, src); err != nil {
		t.Fatal(err)
	}

	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want default 8080", cfg.Port)
	}
	if cfg.Debug {
		t.Error("Debug = true, want default false")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want default %q", cfg.LogLevel, "info")
	}
}

func TestLoadRequiredMissing(t *testing.T) {
	src := config.MapSource{}

	var cfg testConfig
	err := config.Load(&cfg, src)
	if err == nil {
		t.Fatal("expected error for missing required field")
	}
}

func TestLoadNonPointer(t *testing.T) {
	err := config.Load(testConfig{})
	if err == nil {
		t.Fatal("expected error for non-pointer")
	}
}

func TestLoadChainedSources(t *testing.T) {
	src1 := config.MapSource{"PORT": "3000"}
	src2 := config.MapSource{"PORT": "4000", "DATABASE_URL": "sqlite://test"}

	var cfg testConfig
	if err := config.Load(&cfg, config.ChainedSource{src1, src2}); err != nil {
		t.Fatal(err)
	}

	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want 3000 (first source wins)", cfg.Port)
	}
	if cfg.DBURL != "sqlite://test" {
		t.Errorf("DBURL = %q, want %q (from second source)", cfg.DBURL, "sqlite://test")
	}
}

func TestLoadEnvSource(t *testing.T) {
	os.Setenv("TEST_GFASTR_PORT", "9999")
	os.Setenv("TEST_GFASTR_DB", "postgres://env")
	defer os.Unsetenv("TEST_GFASTR_PORT")
	defer os.Unsetenv("TEST_GFASTR_DB")

	type envCfg struct {
		Port int    `config:"TEST_GFASTR_PORT"`
		DB   string `config:"TEST_GFASTR_DB"`
	}

	var cfg envCfg
	if err := config.Load(&cfg); err != nil {
		t.Fatal(err)
	}

	if cfg.Port != 9999 {
		t.Errorf("Port = %d, want 9999", cfg.Port)
	}
	if cfg.DB != "postgres://env" {
		t.Errorf("DB = %q, want %q", cfg.DB, "postgres://env")
	}
}

func TestMustLoadPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustLoad should panic on error")
		}
	}()
	config.MustLoad(&testConfig{})
}
