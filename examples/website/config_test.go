package main

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/config"
)

// loadWebsiteConfig binds PORT and GOFASTR_DEV from a map source.
func TestWebsiteConfigBindsEnv(t *testing.T) {
	cfg, err := loadWebsiteConfig(config.MapSource{
		"PORT":        "9091",
		"GOFASTR_DEV": "true",
	})
	if err != nil {
		t.Fatalf("loadWebsiteConfig: %v", err)
	}
	if cfg.Port != 9091 {
		t.Fatalf("Port = %d, want 9091", cfg.Port)
	}
	if !cfg.Dev {
		t.Fatal("Dev = false, want true")
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
	if cfg.Dev {
		t.Fatal("default Dev = true, want false")
	}
}
