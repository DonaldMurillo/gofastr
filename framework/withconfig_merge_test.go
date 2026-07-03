package framework

import (
	"reflect"
	"testing"
)

// WithConfig must merge into config already set by granular options
// (WithAPIPrefix, WithPublicOpenAPI, …), not replace the whole struct —
// same contract as WithAgentReady vs its granular options.
func TestWithConfigMergesGranular(t *testing.T) {
	app := NewApp(
		WithAPIPrefix("/api"),
		WithPublicOpenAPI(),
		WithConfig(AppConfig{Name: "x"}),
	)
	if app.Config.Name != "x" {
		t.Fatalf("WithConfig should set Name, got %q", app.Config.Name)
	}
	if app.Config.APIPrefix != "/api" {
		t.Fatalf("WithConfig clobbered WithAPIPrefix: APIPrefix = %q", app.Config.APIPrefix)
	}
	if !app.Config.PublicOpenAPI {
		t.Fatal("WithConfig clobbered WithPublicOpenAPI")
	}
}

// Every AppConfig field set explicitly through WithConfig must land on the
// app. Reflection-driven so a future AppConfig field that is forgotten in
// the WithConfig merge fails here instead of silently dropping.
func TestWithConfigCoversEveryField(t *testing.T) {
	var cfg AppConfig
	v := reflect.ValueOf(&cfg).Elem()
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		switch f.Kind() {
		case reflect.String:
			f.SetString("x")
		case reflect.Bool:
			f.SetBool(true)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			f.SetInt(1)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			f.SetUint(1)
		default:
			t.Fatalf("AppConfig field %s has kind %s — extend this test and the WithConfig merge",
				v.Type().Field(i).Name, f.Kind())
		}
	}

	app := NewApp(WithConfig(cfg))
	got := reflect.ValueOf(app.Config)
	for i := 0; i < got.NumField(); i++ {
		if !reflect.DeepEqual(got.Field(i).Interface(), v.Field(i).Interface()) {
			t.Errorf("AppConfig.%s not carried through WithConfig: got %v want %v — field missing from the merge?",
				got.Type().Field(i).Name, got.Field(i).Interface(), v.Field(i).Interface())
		}
	}
}
