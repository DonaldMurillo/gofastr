package auth

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestDefaults_WarnsOnMissingProdSecret pins H1: a production config
// (DevMode=false) with no JWTSecret logs a loud warning — an empty signing key
// means forgeable, restart-unstable sessions.
func TestDefaults_WarnsOnMissingProdSecret(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	c := AuthConfig{DevMode: false} // JWTSecret empty
	c.defaults()

	if !strings.Contains(buf.String(), "no JWTSecret") {
		t.Errorf("expected missing-secret warning, got: %s", buf.String())
	}
}

func TestDefaults_NoWarnWhenSecretSet(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	c := AuthConfig{DevMode: false, JWTSecret: "a-real-secret"}
	c.defaults()

	if strings.Contains(buf.String(), "no JWTSecret") {
		t.Errorf("should not warn when JWTSecret is set: %s", buf.String())
	}
}
