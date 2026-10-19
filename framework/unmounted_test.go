package framework

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/unmounted"
)

// A copy registered by its unmounted.gen.go shows on the App and through
// app_unmounted.
func TestAppUnmountedListsRegisteredCopies(t *testing.T) {
	copy := unmounted.Copy{
		Source:  "github.com/DonaldMurillo/gofastr/battery/fixture-unmounted",
		Version: "v0.91.0",
		Path:    "example.com/app/unmounted/battery/fixture-unmounted",
	}
	unmounted.Register(copy)
	app := NewApp(WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	found := false
	for _, c := range app.Unmounted() {
		found = found || c == copy
	}
	if !found {
		t.Fatalf("App.Unmounted() = %+v, missing %+v", app.Unmounted(), copy)
	}
	res, err := app.MCP.CallTool(context.Background(), "app_unmounted", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	m, _ := res.(map[string]any)
	copies, _ := m["unmounted"].([]unmounted.Copy)
	found = false
	for _, c := range copies {
		found = found || c == copy
	}
	if !found || m["count"] != len(copies) {
		t.Fatalf("app_unmounted = %#v", res)
	}
}
