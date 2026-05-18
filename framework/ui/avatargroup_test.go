package ui

import (
	"strings"
	"testing"
)

func TestAvatarGroupRendersAll(t *testing.T) {
	out := string(AvatarGroup(AvatarGroupConfig{
		Avatars: []AvatarConfig{{Name: "Alice"}, {Name: "Bob"}, {Name: "Carol"}},
	}))
	if !strings.Contains(out, `role="group"`) {
		t.Errorf("expected role=group, got: %s", out)
	}
	if !strings.Contains(out, `aria-label="Avatars"`) {
		t.Errorf("expected aria-label, got: %s", out)
	}
	for _, name := range []string{"Alice", "Bob", "Carol"} {
		if !strings.Contains(out, name) {
			t.Errorf("expected %q in output, got: %s", name, out)
		}
	}
	if strings.Contains(out, "ui-avatar-group__overflow") {
		t.Error("did not expect overflow indicator")
	}
}

func TestAvatarGroupOverflow(t *testing.T) {
	avatars := []AvatarConfig{
		{Name: "Alice"}, {Name: "Bob"}, {Name: "Carol"},
		{Name: "Dave"}, {Name: "Eve"}, {Name: "Frank"}, {Name: "Grace"},
	}
	out := string(AvatarGroup(AvatarGroupConfig{
		Avatars: avatars,
		Max:     5,
	}))
	if !strings.Contains(out, "ui-avatar-group__overflow") {
		t.Errorf("expected overflow indicator, got: %s", out)
	}
	if !strings.Contains(out, ">+2<") {
		t.Errorf("expected +2 overflow, got: %s", out)
	}
	if !strings.Contains(out, `aria-label="2 more"`) {
		t.Errorf("expected aria-label='2 more', got: %s", out)
	}
	// Frank / Grace should NOT be rendered (they're past Max=5).
	if strings.Contains(out, "Frank") || strings.Contains(out, "Grace") {
		t.Errorf("trailing avatars must not render past Max, got: %s", out)
	}
}

func TestAvatarGroupDefaultMaxFive(t *testing.T) {
	avatars := make([]AvatarConfig, 7)
	for i := range avatars {
		avatars[i] = AvatarConfig{Name: string(rune('A'+i)) + "Person"}
	}
	out := string(AvatarGroup(AvatarGroupConfig{Avatars: avatars})) // Max=0 → 5
	if !strings.Contains(out, ">+2<") {
		t.Errorf("expected +2 overflow with default Max=5, got: %s", out)
	}
}

func TestAvatarGroupSizePropagates(t *testing.T) {
	out := string(AvatarGroup(AvatarGroupConfig{
		Avatars: []AvatarConfig{{Name: "Alice"}, {Name: "Bob"}},
		Size:    AvatarLg,
	}))
	if !strings.Contains(out, "ui-avatar-group--lg") {
		t.Errorf("expected ui-avatar-group--lg class, got: %s", out)
	}
	if !strings.Contains(out, "ui-avatar--lg") {
		t.Errorf("expected Size to propagate to children (ui-avatar--lg), got: %s", out)
	}
}

func TestAvatarGroupChildSizeOverrides(t *testing.T) {
	out := string(AvatarGroup(AvatarGroupConfig{
		Avatars: []AvatarConfig{
			{Name: "Alice", Size: AvatarXl},
			{Name: "Bob"},
		},
		Size: AvatarLg,
	}))
	if !strings.Contains(out, "ui-avatar--xl") {
		t.Errorf("expected child Size=xl to be preserved, got: %s", out)
	}
}

func TestAvatarGroupPanicsOnEmpty(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic on empty Avatars")
		}
	}()
	AvatarGroup(AvatarGroupConfig{})
}

func TestAvatarGroupCustomLabel(t *testing.T) {
	out := string(AvatarGroup(AvatarGroupConfig{
		Avatars: []AvatarConfig{{Name: "Alice"}},
		Label:   "Project team",
	}))
	if !strings.Contains(out, `aria-label="Project team"`) {
		t.Errorf("expected custom Label, got: %s", out)
	}
}
