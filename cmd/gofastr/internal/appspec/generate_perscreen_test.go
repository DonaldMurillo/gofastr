package appspec

import (
	"testing"
)

// TestScreenFileNameCollisionGuard ensures a screen named "shared" is
// re-prefixed so it never shadows the screen_shared.go nodeComponent helper.
func TestScreenFileNameCollisionGuard(t *testing.T) {
	if got := ScreenFileName("shared"); got != "screen_screen_shared.go" {
		t.Errorf("screenFileName(%q) = %q, want screen_screen_shared.go", "shared", got)
	}
	if got := ScreenFileName("home"); got != "screen_home.go" {
		t.Errorf("screenFileName(%q) = %q, want screen_home.go", "home", got)
	}
}
