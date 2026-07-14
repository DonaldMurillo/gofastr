package main

import (
	"strings"
	"testing"
)

func TestScreenMockUsesFrameworkStatusBadges(t *testing.T) {
	h := string(screenMock())
	for _, want := range []string{"ui-badge--success", "ui-badge--neutral"} {
		if !strings.Contains(h, want) {
			t.Errorf("screen mock missing framework status badge %q\n%s", want, h)
		}
	}
	if strings.Contains(h, "mock-badge") {
		t.Fatalf("screen mock must not recreate framework badge styling\n%s", h)
	}
}
