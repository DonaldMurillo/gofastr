package config_test

import (
	"os"
	"strings"
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

func TestEnvSourceDistinguishesEmpty(t *testing.T) {
	const key = "TEST_GFASTR_EMPTY"
	os.Setenv(key, "")
	defer os.Unsetenv(key)
	v, ok := config.EnvSource{}.Get(key)
	if !ok {
		t.Fatalf("expected ok=true for set-empty env, got %v", ok)
	}
	if v != "" {
		t.Fatalf("expected empty value, got %q", v)
	}
}

func TestDurationParsesMillis(t *testing.T) {
	type cfg struct {
		Ms time.Duration `config:"MS"`
	}
	var c cfg
	src := config.MapSource{"MS": "500ms"}
	if err := config.Load(&c, src); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Ms != 500*time.Millisecond {
		t.Fatalf("Ms = %v, want 500ms", c.Ms)
	}
}

func TestDurationParsesNanosAndMicros(t *testing.T) {
	type cfg struct {
		A time.Duration `config:"A"`
		B time.Duration `config:"B"`
	}
	var c cfg
	src := config.MapSource{"A": "100ns", "B": "200us"}
	if err := config.Load(&c, src); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.A != 100*time.Nanosecond {
		t.Fatalf("A = %v, want 100ns", c.A)
	}
	if c.B != 200*time.Microsecond {
		t.Fatalf("B = %v, want 200us", c.B)
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

func TestRequiredErrorNamesFieldAndKey(t *testing.T) {
	type cfg struct {
		APIKey string `config:"API_KEY" required:"true"`
	}
	var c cfg
	err := config.Load(&c, config.MapSource{})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "APIKey") {
		t.Errorf("error %q missing Go field name APIKey", msg)
	}
	if !strings.Contains(msg, "API_KEY") {
		t.Errorf("error %q missing env key API_KEY", msg)
	}
}

func TestNestedStructPrefixed(t *testing.T) {
	type DBConfig struct {
		Host string `config:"HOST" required:"true"`
		Port int    `config:"PORT" default:"5432"`
	}
	type App struct {
		DB DBConfig
	}
	src := config.MapSource{
		"DB_HOST": "db.example.com",
		"DB_PORT": "6543",
	}
	var a App
	if err := config.Load(&a, src); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if a.DB.Host != "db.example.com" {
		t.Errorf("DB.Host = %q, want db.example.com", a.DB.Host)
	}
	if a.DB.Port != 6543 {
		t.Errorf("DB.Port = %d, want 6543", a.DB.Port)
	}
}

func TestSensitiveValueRedacted(t *testing.T) {
	type cfg struct {
		Password int `config:"PASSWORD" sensitive:"true"`
	}
	var c cfg
	src := config.MapSource{"PASSWORD": "supersecret-not-an-int"}
	err := config.Load(&c, src)
	if err == nil {
		t.Fatal("expected error parsing bad int")
	}
	if strings.Contains(err.Error(), "supersecret-not-an-int") {
		t.Errorf("error leaked sensitive value: %q", err.Error())
	}
}

func TestValidateHookErrors(t *testing.T) {
	var c validatedConfig
	src := config.MapSource{"NAME": "bad"}
	err := config.Load(&c, src)
	if err == nil {
		t.Fatal("expected Validate() error")
	}
	if !strings.Contains(err.Error(), "name rejected") {
		t.Errorf("error %q missing Validate message", err.Error())
	}
}

func TestMustLoadPanicsOnValidate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustLoad should panic on Validate error")
		}
	}()
	var c validatedConfig
	config.MustLoad(&c, config.MapSource{"NAME": "bad"})
}

type validatedConfig struct {
	Name string `config:"NAME"`
}

func (v *validatedConfig) Validate() error {
	if v.Name == "bad" {
		return errBadName
	}
	return nil
}

var errBadName = stringErr("name rejected")

type stringErr string

func (s stringErr) Error() string { return string(s) }
