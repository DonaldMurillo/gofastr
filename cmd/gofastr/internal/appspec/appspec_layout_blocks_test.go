package appspec

import (
	"strings"
	"testing"
)

func TestAppSpecLayoutBlocksRejectNonTokenGap(t *testing.T) {
	bp := AppSpec{Screens: []AppSpecScreen{{Name: "home", Route: "/", Body: []AppSpecBlock{{
		Kind: "stack", Props: map[string]any{"gap": "23px"},
	}}}}}
	err := ValidateAppSpec(bp)
	if err == nil || !strings.Contains(err.Error(), "not a design token") {
		t.Fatalf("bad layout gap error = %v", err)
	}
}
