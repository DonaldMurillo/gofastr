package framework

import "testing"

// WithConfig replaces the whole AppConfig. An earlier granular option's field
// does NOT survive a later WithConfig: fields WithConfig leaves at the zero
// value become the zero value, not "keep whatever a prior option set".
func TestWithConfigReplacesWholeStruct(t *testing.T) {
	app := NewApp(
		WithAPIPrefix("/api"),
		WithPublicOpenAPI(),
		WithConfig(AppConfig{Name: "x"}),
	)
	if app.Config.Name != "x" {
		t.Fatalf("Name = %q, want %q", app.Config.Name, "x")
	}
	if app.Config.APIPrefix != "" {
		t.Fatalf("APIPrefix = %q, want zero — WithConfig must replace the struct, not merge", app.Config.APIPrefix)
	}
	if app.Config.PublicOpenAPI {
		t.Fatal("PublicOpenAPI = true, want false — WithConfig must replace the struct, not merge")
	}
}

// Option-order: a granular setter placed AFTER WithConfig overrides the field
// it sets. This is the replacement-contract corollary, later options win.
func TestWithConfigGranularSetterAfterWins(t *testing.T) {
	app := NewApp(
		WithConfig(AppConfig{Name: "x"}),
		WithAPIPrefix("/api"),
		WithPublicOpenAPI(),
	)
	if app.Config.Name != "x" {
		t.Fatalf("Name = %q, want %q", app.Config.Name, "x")
	}
	if app.Config.APIPrefix != "/api" {
		t.Fatalf("APIPrefix = %q, want %q — a granular setter after WithConfig must win", app.Config.APIPrefix, "/api")
	}
	if !app.Config.PublicOpenAPI {
		t.Fatal("PublicOpenAPI = false, want true — a granular setter after WithConfig must win")
	}
}

// WithConfig carrying every field lands every field verbatim (no per-field
// allow-list to forget when AppConfig grows). A future field that WithConfig
// silently dropped would fail here.
func TestWithConfigCopiesEveryField(t *testing.T) {
	cfg := AppConfig{
		Name:                  "every",
		JSONCase:              CaseSnake,
		DebugEndpoints:        true,
		NoLLMMD:               true,
		PublicOpenAPI:         true,
		APIPrefix:             "/api",
		RequestTimeout:        7,
		DisableRequestTimeout: true,
		ShutdownTimeout:       9,
		DisableSignalHandling: true,
	}
	app := NewApp(WithConfig(cfg))
	got := app.Config
	if got != cfg {
		t.Fatalf("WithConfig did not copy AppConfig verbatim:\n got=%+v\nwant=%+v", got, cfg)
	}
}
