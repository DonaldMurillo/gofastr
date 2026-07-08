package ui

import (
	"strings"
	"testing"
)

func TestToggleActionIdleMarkup(t *testing.T) {
	got := string(ToggleAction(ToggleActionConfig{
		Endpoint:       "/api/follow",
		IdleLabel:      "Follow",
		CommittedLabel: "Following ✓",
	}))
	for _, want := range []string{
		`data-fui-comp="ui-toggle-action"`,
		`data-fui-toggle-endpoint="/api/follow"`,
		`data-fui-toggle-method="POST"`,
		`data-state="idle"`,
		`aria-pressed="false"`,
		`type="button"`,
		`class="ui-button ui-toggle-action"`,
		`data-fui-toggle-idle`,
		`data-fui-toggle-committed`,
		`>Follow<`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// The committed span ships hidden when SSR state is idle; the idle
	// span must NOT be hidden.
	if !strings.Contains(got, `data-fui-toggle-committed"`) &&
		!strings.Contains(got, `data-fui-toggle-committed=`) {
		t.Fatalf("committed span marker missing:\n%s", got)
	}
	committedSpan := got[strings.Index(got, "data-fui-toggle-committed"):]
	committedSpan = committedSpan[:strings.Index(committedSpan, ">")]
	if !strings.Contains(committedSpan, "hidden") {
		t.Errorf("committed span not hidden in idle state:\n%s", got)
	}
	idleSpan := got[strings.Index(got, "data-fui-toggle-idle"):]
	idleSpan = idleSpan[:strings.Index(idleSpan, ">")]
	if strings.Contains(idleSpan, "hidden") {
		t.Errorf("idle span hidden in idle state:\n%s", got)
	}
}

func TestToggleActionCommittedSSR(t *testing.T) {
	got := string(ToggleAction(ToggleActionConfig{
		Endpoint:       "/api/follow",
		IdleLabel:      "Follow",
		CommittedLabel: "Following",
		Committed:      true,
	}))
	if !strings.Contains(got, `data-state="committed"`) {
		t.Errorf("missing committed data-state:\n%s", got)
	}
	if !strings.Contains(got, `aria-pressed="true"`) {
		t.Errorf("missing aria-pressed=true:\n%s", got)
	}
	idleSpan := got[strings.Index(got, "data-fui-toggle-idle"):]
	idleSpan = idleSpan[:strings.Index(idleSpan, ">")]
	if !strings.Contains(idleSpan, "hidden") {
		t.Errorf("idle span not hidden in committed state:\n%s", got)
	}
	committedSpan := got[strings.Index(got, "data-fui-toggle-committed"):]
	committedSpan = committedSpan[:strings.Index(committedSpan, ">")]
	if strings.Contains(committedSpan, "hidden") {
		t.Errorf("committed span hidden in committed state:\n%s", got)
	}
}

func TestToggleActionGroupUntoggle(t *testing.T) {
	got := string(ToggleAction(ToggleActionConfig{
		Endpoint:         "/api/plan/pro",
		Method:           "PUT",
		IdleLabel:        "Choose Pro",
		CommittedLabel:   "Current plan",
		Group:            "plan",
		AllowUntoggle:    true,
		UntoggleEndpoint: "/api/plan/clear",
	}))
	for _, want := range []string{
		`data-fui-toggle-group="plan"`,
		`data-fui-toggle-allow-untoggle="true"`,
		`data-fui-toggle-untoggle-endpoint="/api/plan/clear"`,
		`data-fui-toggle-method="PUT"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestToggleActionUntoggleEndpointImplies(t *testing.T) {
	// Setting UntoggleEndpoint without AllowUntoggle still opts in —
	// a reverse endpoint is meaningless on a sticky button.
	got := string(ToggleAction(ToggleActionConfig{
		Endpoint:         "/api/follow",
		IdleLabel:        "Follow",
		CommittedLabel:   "Following",
		UntoggleEndpoint: "/api/unfollow",
	}))
	if !strings.Contains(got, `data-fui-toggle-allow-untoggle="true"`) {
		t.Errorf("UntoggleEndpoint should imply allow-untoggle:\n%s", got)
	}
}

func TestToggleActionVariantClass(t *testing.T) {
	got := string(ToggleAction(ToggleActionConfig{
		Endpoint:       "/x",
		IdleLabel:      "A",
		CommittedLabel: "B",
		Variant:        ButtonSecondary,
		Class:          "extra",
		ID:             "t1",
	}))
	for _, want := range []string{"ui-button--secondary", "extra", `id="t1"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestToggleActionBadVariantPanics(t *testing.T) {
	// ToggleAction shares Button's variant/size sets and must validate
	// them the same way — a typo'd variant panics at render time
	// instead of silently rendering an unstyled button.
	wantPanic(t, "ToggleAction unknown Variant", func() {
		ToggleAction(ToggleActionConfig{
			Endpoint: "/x", IdleLabel: "A", CommittedLabel: "B",
			Variant: ButtonVariant("nope"),
		})
	})
	wantPanic(t, "ToggleAction unknown Size", func() {
		ToggleAction(ToggleActionConfig{
			Endpoint: "/x", IdleLabel: "A", CommittedLabel: "B",
			Size: ButtonSize("nope"),
		})
	})
}

func TestToggleActionPanics(t *testing.T) {
	cases := []struct {
		name string
		cfg  ToggleActionConfig
	}{
		{"no endpoint", ToggleActionConfig{IdleLabel: "A", CommittedLabel: "B"}},
		{"no idle label", ToggleActionConfig{Endpoint: "/x", CommittedLabel: "B"}},
		{"no committed label", ToggleActionConfig{Endpoint: "/x", IdleLabel: "A"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("expected panic for %s", tc.name)
				}
			}()
			ToggleAction(tc.cfg)
		})
	}
}
