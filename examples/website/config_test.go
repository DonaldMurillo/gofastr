package main

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/config"
)

// loadWebsiteConfig binds PORT from a map source. (GOFASTR_DEV is now
// read inside framework.NewApp / uihost.New rather than this app's
// config, so the website no longer carries its own Dev flag.)
func TestWebsiteConfigBindsEnv(t *testing.T) {
	cfg, err := loadWebsiteConfig(config.MapSource{
		"PORT": "9091",
	})
	if err != nil {
		t.Fatalf("loadWebsiteConfig: %v", err)
	}
	if cfg.Port != 9091 {
		t.Fatalf("Port = %d, want 9091", cfg.Port)
	}
	if cfg.Addr() != ":9091" {
		t.Fatalf("Addr = %q, want :9091", cfg.Addr())
	}
}

// Defaults apply when env is empty.
func TestWebsiteConfigDefaults(t *testing.T) {
	cfg, err := loadWebsiteConfig(config.MapSource{})
	if err != nil {
		t.Fatalf("loadWebsiteConfig: %v", err)
	}
	if cfg.Port != 8082 {
		t.Fatalf("default Port = %d, want 8082", cfg.Port)
	}
}
