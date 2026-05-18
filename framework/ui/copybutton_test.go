package ui

import (
	"strings"
	"testing"
)

func TestCopyButtonBasic(t *testing.T) {
	out := string(CopyButton(CopyButtonConfig{Target: "#code-1"}))
	wants := []string{
		`data-fui-copy-text-from="#code-1"`,
		`data-fui-copy-announce="Copied"`,
		`type="button"`,
		`ui-copy-btn__label`,
		`>Copy<`,
		`ui-copy-btn__copied`,
		`aria-hidden="true"`,
		`role="status"`,
		`aria-live="polite"`,
		`data-fui-copy-status=""`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("CopyButton missing %q\nout: %s", w, out)
		}
	}
}

func TestCopyButtonPanicsWithoutTarget(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic on missing Target")
		}
	}()
	CopyButton(CopyButtonConfig{})
}

func TestCopyButtonCustomLabels(t *testing.T) {
	out := string(CopyButton(CopyButtonConfig{
		Target:       "#tok",
		Label:        "Copy token",
		CopiedLabel:  "Token copied",
		AnnounceText: "Token copied to clipboard",
	}))
	if !strings.Contains(out, ">Copy token<") {
		t.Errorf("expected custom Label, got: %s", out)
	}
	if !strings.Contains(out, ">Token copied<") {
		t.Errorf("expected custom CopiedLabel, got: %s", out)
	}
	if !strings.Contains(out, `data-fui-copy-announce="Token copied to clipboard"`) {
		t.Errorf("expected custom AnnounceText, got: %s", out)
	}
}

func TestCopyButtonIconOnly(t *testing.T) {
	out := string(CopyButton(CopyButtonConfig{
		Target:   "#code",
		IconOnly: true,
	}))
	if !strings.Contains(out, `aria-label="Copy to clipboard"`) {
		t.Errorf("icon-only must have default aria-label, got: %s", out)
	}
	if strings.Contains(out, "ui-copy-btn__label") {
		t.Errorf("icon-only must not render visible label span, got: %s", out)
	}
	if !strings.Contains(out, "ui-copy-btn--icon") {
		t.Errorf("expected icon modifier class, got: %s", out)
	}
}

func TestCopyButtonAriaLabelOverride(t *testing.T) {
	out := string(CopyButton(CopyButtonConfig{
		Target:    "#x",
		IconOnly:  true,
		AriaLabel: "Copy API key",
	}))
	if !strings.Contains(out, `aria-label="Copy API key"`) {
		t.Errorf("expected custom AriaLabel, got: %s", out)
	}
}
