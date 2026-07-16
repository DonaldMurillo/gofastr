package protocol_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func TestSetAppConfigRejectsMissingNestedConfig(t *testing.T) {
	tools := newTools(t)
	res := tools.SetAppConfig(context.Background(), protocol.SetAppConfigArgs{})
	if res.OK || res.Kind != "validation" {
		t.Fatalf("empty app config = %+v, want validation failure", res)
	}
	if !strings.Contains(res.Hint, `{"config":`) {
		t.Fatalf("hint must show the transport wrapper: %+v", res)
	}
	if got := tools.Live().Session().World.App.Name; got != "" {
		t.Fatalf("invalid config mutated world name to %q", got)
	}
}

func TestSetAppConfigAcceptsCurrentShapeAndDefaultsAPI(t *testing.T) {
	tools := newTools(t)
	res := tools.SetAppConfig(context.Background(), protocol.SetAppConfigArgs{Config: world.AppConfig{
		Name: "forge", Module: "example.com/forge",
	}})
	if !res.OK {
		t.Fatalf("SetAppConfig: %+v", res)
	}
	app := tools.Live().Session().World.App
	if app.Name != "forge" || app.Module != "example.com/forge" || app.APIPrefix != "api" {
		t.Fatalf("app config not preserved/defaulted: %+v", app)
	}
}
