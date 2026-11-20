package config_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/config"
)

func TestPresetSensitiveRedactedInValidator(t *testing.T) {
	for _, src := range []config.MapSource{{}, {"DSN": ""}} {
		c := nestedSecretCfg{DSN: "preset-credential", Plain: "diagnostic-context"}
		err := config.Load(&c, src)
		if err == nil || strings.Contains(err.Error(), c.DSN) || !strings.Contains(err.Error(), "diagnostic-context") {
			t.Fatalf("Load = %v, want redacted preset and diagnostic context", err)
		}
	}
}

func TestSensitiveRedactedInOtherFieldParseError(t *testing.T) {
	for _, secret := range []string{"parse-credential", "quoted\"credential"} {
		var c struct {
			Port  int    `config:"PORT"`
			Token string `config:"TOKEN" sensitive:"true"`
		}
		err := config.Load(&c, config.MapSource{"PORT": secret, "TOKEN": secret})
		if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "(redacted)") || !strings.Contains(err.Error(), "Port") {
			t.Fatalf("Load = %v, want redacted parse error naming Port", err)
		}
	}
}

func TestUnsetSensitiveIntPreservesParseErrorDigits(t *testing.T) {
	var c struct {
		Secret int `config:"SECRET" sensitive:"true"`
		Port   int `config:"PORT"`
	}
	err := config.Load(&c, config.MapSource{"PORT": "0-invalid"})
	if err == nil || !strings.Contains(err.Error(), "0-invalid") || strings.Contains(err.Error(), "(redacted)") {
		t.Fatalf("Load = %v, want unchanged parse error containing 0-invalid", err)
	}
}
