package embed

import (
	"strings"
	"testing"
)

// testScreen is a minimal embed.Screen for tests in THIS package, which cannot
// import core-ui/app: the layering rule this package exists under. *app.Screen
// satisfies embed.Screen structurally in production code.
type testScreen struct {
	path string
}

func (s testScreen) RoutePath() string { return s.path }

// A surface renders a screen, not a path string. The screen is required, and
// its route is validated exactly as the old Path string was: absolute,
// normalized, not "/", and not covering a reserved prefix.
func TestSurfaceCarriesAScreenNotAPath(t *testing.T) {
	cases := []struct {
		name    string
		screen  Screen
		wantErr string
	}{
		{
			name:    "nil screen is a boot error",
			screen:  nil,
			wantErr: "Screen is required",
		},
		{
			name:    "root route reaches the whole app",
			screen:  testScreen{"/"},
			wantErr: "whole app",
		},
		{
			name:    "reserved prefix is rejected",
			screen:  testScreen{"/auth"},
			wantErr: "covers",
		},
		{
			name:    "another reserved prefix",
			screen:  testScreen{"/admin"},
			wantErr: "covers",
		},
		{
			name:    "relative route is rejected",
			screen:  testScreen{"reports"},
			wantErr: "absolute app route",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(Config{
				Surfaces: []Surface{{
					Name:    "reports",
					Screen:  tc.screen,
					Origins: []string{"https://acme.example"},
				}},
				BurnStore: NewMemoryBurnStore(),
			})
			if err == nil {
				t.Fatalf("New accepted a surface with %s; want an error mentioning %q", tc.name, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("New(%s) error = %q; want it to mention %q", tc.name, err.Error(), tc.wantErr)
			}
		})
	}
}

// The screen's route is normalized (trailing slash fixed) and the resolved path
// is what MayReach compares against, unchanged from the old string-Path world.
func TestSurfaceScreenRouteIsResolvedAndReachable(t *testing.T) {
	h, err := New(Config{
		Surfaces: []Surface{{
			Name:    "reports",
			Screen:  testScreen{"/reports/"},
			Origins: []string{"https://acme.example"},
		}},
		BurnStore: NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s, ok := h.Lookup("reports")
	if !ok {
		t.Fatal("Lookup(reports) miss")
	}
	// Trailing slash normalized away.
	if got, want := s.Path(), "/reports"; got != want {
		t.Fatalf("ResolvedSurface.Path() = %q, want %q (trailing slash should be normalized)", got, want)
	}
	// The surface's own subtree is reachable, a sibling prefix is not.
	if !s.MayReach("/reports/42") {
		t.Error("MayReach(/reports/42) = false; the surface's own subtree must be reachable")
	}
	if s.MayReach("/reports-archive") {
		t.Error("MayReach(/reports-archive) = true; a sibling segment boundary must not be reachable")
	}
	// The screen value is retained, so a caller can follow surface → screen →
	// component tree as a Go value rather than resolving a string.
	if s.Screen == nil {
		t.Error("ResolvedSurface.Screen is nil; the surface must carry the screen value")
	}
	if got := s.Screen.RoutePath(); got != "/reports/" {
		t.Errorf("ResolvedSurface.Screen.RoutePath() = %q, want the original /reports/", got)
	}
}
