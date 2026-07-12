package ui

import (
	"strings"
	"testing"
)

func TestAvatarNoStatusByDefault(t *testing.T) {
	out := string(Avatar(AvatarConfig{Name: "Ada Lovelace"}))
	if strings.Contains(out, "ui-avatar__status") {
		t.Errorf("expected no status dot by default, got: %s", out)
	}
	if strings.Contains(out, "ui-avatar--has-status") {
		t.Errorf("did not expect has-status modifier, got: %s", out)
	}
}

func TestAvatarStatusRendersDot(t *testing.T) {
	out := string(Avatar(AvatarConfig{Name: "Ada Lovelace", Status: AvatarOnline}))
	if !strings.Contains(out, "ui-avatar__status ui-avatar__status--online") {
		t.Errorf("expected online status dot, got: %s", out)
	}
	if !strings.Contains(out, "ui-avatar--has-status") {
		t.Errorf("expected has-status modifier, got: %s", out)
	}
	// Bare aria-label on a span is axe-rejected; the dot uses role=img.
	if !strings.Contains(out, `role="img"`) || !strings.Contains(out, `aria-label="online"`) {
		t.Errorf("expected role=img + aria-label default, got: %s", out)
	}
}

func TestAvatarStatusVariants(t *testing.T) {
	for _, s := range []AvatarStatus{AvatarOnline, AvatarAway, AvatarBusy, AvatarOffline} {
		out := string(Avatar(AvatarConfig{Name: "X", Status: s}))
		want := "ui-avatar__status--" + string(s)
		if !strings.Contains(out, want) {
			t.Errorf("status %q: expected %q, got: %s", s, want, out)
		}
	}
}

func TestAvatarStatusLabelOverride(t *testing.T) {
	out := string(Avatar(AvatarConfig{
		Name: "Ada", Status: AvatarBusy, StatusLabel: "In a meeting",
	}))
	if !strings.Contains(out, `aria-label="In a meeting"`) {
		t.Errorf("expected custom status label, got: %s", out)
	}
}

func TestAvatarStatusWithImage(t *testing.T) {
	out := string(Avatar(AvatarConfig{
		Name: "Ada", Src: "/a.png", Status: AvatarAway,
	}))
	if !strings.Contains(out, "ui-avatar__img") {
		t.Errorf("expected image, got: %s", out)
	}
	if !strings.Contains(out, "ui-avatar__status--away") {
		t.Errorf("expected status dot alongside image, got: %s", out)
	}
}
