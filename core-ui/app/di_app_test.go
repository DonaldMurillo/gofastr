package app

import "testing"

type diTestConfig struct{ Env string }
type diTestLogger struct{ Prefix string }
type diTestDB struct{ DSN string }

type diTestService struct {
	Log *diTestLogger `inject:""`
	DB  *diTestDB     `inject:""`
}

func TestDI_AppProvideAndResolve(t *testing.T) {
	app := NewApp("test")
	if err := app.Provide(func() *diTestConfig {
		return &diTestConfig{Env: "testing"}
	}); err != nil {
		t.Fatalf("App.Provide: %v", err)
	}

	var cfg *diTestConfig
	if err := app.Container.Resolve(&cfg); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.Env != "testing" {
		t.Errorf("expected Env=\"testing\", got %q", cfg.Env)
	}
}

func TestDI_AppInject(t *testing.T) {
	app := NewApp("test")
	if err := app.Provide(&diTestLogger{Prefix: "app"}); err != nil {
		t.Fatalf("Provide logger: %v", err)
	}
	if err := app.Provide(&diTestDB{DSN: "test://db"}); err != nil {
		t.Fatalf("Provide db: %v", err)
	}

	svc := &diTestService{}
	if err := app.Inject(svc); err != nil {
		t.Fatalf("App.Inject: %v", err)
	}
	if svc.Log == nil || svc.Log.Prefix != "app" {
		t.Error("App.Inject should fill tagged Log field")
	}
	if svc.DB == nil || svc.DB.DSN != "test://db" {
		t.Error("App.Inject should fill tagged DB field")
	}
}
