package framework

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// WithConfig replaces the whole AppConfig, so a granular option placed before
// it is silently discarded, the exact paste-point mistake the old `gofastr
// init` scaffold invited (WithConfig last, so "next to WithDB" meant "before
// WithConfig"). Replace semantics stay: a merge could not tell an explicit
// zero from an unset field, and existing callers rely on WithConfig being
// authoritative. Instead the discard is made loud: NewApp warns, naming each
// field an earlier option set that the replacement returned to zero.

func warnBuffer() (*bytes.Buffer, *slog.Logger) {
	var buf bytes.Buffer
	return &buf, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestWithConfigWarnsNamingDiscardedFields(t *testing.T) {
	buf, logger := warnBuffer()
	NewApp(
		WithLogger(logger),
		WithPublicOpenAPI(),
		WithAPIPrefix("/api"),
		WithConfig(AppConfig{Name: "app"}),
	)
	for _, field := range []string{"PublicOpenAPI", "APIPrefix"} {
		if !strings.Contains(buf.String(), field) {
			t.Errorf("warning must name discarded field %s — log output: %q", field, buf.String())
		}
	}
}

func TestWithConfigFirstStaysSilent(t *testing.T) {
	buf, logger := warnBuffer()
	NewApp(
		WithLogger(logger),
		WithConfig(AppConfig{Name: "app"}),
		WithPublicOpenAPI(),
	)
	if s := buf.String(); strings.Contains(s, "WithConfig") {
		t.Errorf("granular option AFTER WithConfig loses nothing — no warning expected, got: %q", s)
	}
}

// The JSONCase default NewApp seeds before options run is not "an earlier
// option"; every plain WithConfig call replaces it and must not warn.
func TestWithConfigAloneStaysSilent(t *testing.T) {
	buf, logger := warnBuffer()
	NewApp(
		WithLogger(logger),
		WithConfig(AppConfig{Name: "app"}),
	)
	if s := buf.String(); strings.Contains(s, "WithConfig") {
		t.Errorf("replacing only the NewApp default seed must not warn, got: %q", s)
	}
}

// A later option that re-sets the field means nothing was ultimately lost;
// the warning is filtered against the FINAL config, not the mid-apply state.
func TestWithConfigSilentWhenLaterOptionRestoresField(t *testing.T) {
	buf, logger := warnBuffer()
	app := NewApp(
		WithLogger(logger),
		WithAPIPrefix("/api"),
		WithConfig(AppConfig{Name: "app"}),
		WithAPIPrefix("/api"),
	)
	if s := buf.String(); strings.Contains(s, "APIPrefix") {
		t.Errorf("field restored after WithConfig — no warning expected, got: %q", s)
	}
	if app.Config.APIPrefix != "/api" {
		t.Errorf("final APIPrefix = %q, want %q", app.Config.APIPrefix, "/api")
	}
}
