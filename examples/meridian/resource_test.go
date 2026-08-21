// Seeded by `gofastr generate --from=gofastr.yml`, then hand-evolved.
// Meridian is hand-maintained and NOT regenerable. See doc.go.

package main

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui/resource"
)

func TestResourceConfigUsesFrameworkEngine(t *testing.T) {
	var cfg ResourceConfig = resource.Config{Title: "Invoices"}
	cfg = cfg.WithHeading("Recent invoices").WithLimit(8)
	if cfg.Heading != "Recent invoices" || cfg.PageSize != 8 {
		t.Fatalf("shared resource config options were not applied: %#v", cfg)
	}
}

func TestMissingDashboardResourceFailsClosed(t *testing.T) {
	if got := statValue(context.Background(), "missing", "count", "", "", ""); got != "—" {
		t.Fatalf("statValue for missing resource = %q, want em dash", got)
	}
	if bars := groupBars(context.Background(), "missing", "status"); len(bars) != 0 {
		t.Fatalf("groupBars for missing resource = %#v, want empty", bars)
	}
}
