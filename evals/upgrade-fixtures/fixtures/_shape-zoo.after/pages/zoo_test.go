package pages

import "testing"

func TestZoo(t *testing.T) {
	t.Setenv("ZOO_BUTTON_CLASS", "fui-button")
	if len(All()) == 0 {
		t.Fatalf("no ui-button markup rendered")
	}
	t.Run("ui-button", func(t *testing.T) {})
}
