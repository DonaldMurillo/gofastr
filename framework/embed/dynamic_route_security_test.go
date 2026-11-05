package embed

import (
	"strings"
	"testing"
)

func TestEmbedRejectsDynamicRoutes(t *testing.T) {
	for _, route := range []string{"/reports/:id", "/reports/:id:int", "/teams/{team}/summary", "/catalog/{slug:string}"} {
		t.Run(route, func(t *testing.T) {
			host, err := New(Config{
				Surfaces: []Surface{{
					Name:    "customer-summary",
					Screen:  testScreen{route},
					Origins: []string{"https://customer.example"},
				}},
				BurnStore: NewMemoryBurnStore(),
			})
			if err == nil || host != nil {
				t.Fatalf("New accepted dynamic screen route %q", route)
			}
			for _, detail := range []string{"customer-summary", route, "dynamic", "static"} {
				if !strings.Contains(err.Error(), detail) {
					t.Errorf("boot error %q does not name %q", err, detail)
				}
			}
		})
	}
	for _, route := range []string{"/reports/monthly", "/teams/overview/", "/catalog/customer-summary"} {
		t.Run(route, func(t *testing.T) {
			_, err := New(Config{
				Surfaces:  []Surface{{Name: "summary", Screen: testScreen{route}, Origins: []string{"https://customer.example"}}},
				BurnStore: NewMemoryBurnStore(),
			})
			if err != nil {
				t.Fatalf("New refused static screen route: %v", err)
			}
		})
	}
}
