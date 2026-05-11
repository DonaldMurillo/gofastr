package style

import (
	"strings"
	"testing"
)

func TestComponentSheetScopesSimpleSelectors(t *testing.T) {
	ss := NewComponentSheet("modal", DefaultTheme())
	ss.Rule(".header").Set("font-weight", "700").End()
	ss.Rule(".body").Set("padding", "{spacing.lg}").End()
	got, err := ss.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(got, `[data-fui-comp="modal"] .header`) {
		t.Errorf("missing scoped .header: %s", got)
	}
	if !strings.Contains(got, `[data-fui-comp="modal"] .body`) {
		t.Errorf("missing scoped .body: %s", got)
	}
	if !strings.Contains(got, "padding: 16px") {
		t.Errorf("theme token not resolved: %s", got)
	}
}

func TestComponentSheetCompoundSelector(t *testing.T) {
	ss := NewComponentSheet("modal", DefaultTheme())
	ss.Rule(".a, .b, .c").Set("color", "red").End()
	got, err := ss.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, want := range []string{
		`[data-fui-comp="modal"] .a`,
		`[data-fui-comp="modal"] .b`,
		`[data-fui-comp="modal"] .c`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}

func TestComponentSheetPseudoAndChild(t *testing.T) {
	ss := NewComponentSheet("modal", DefaultTheme())
	ss.Rule(".btn").
		Set("color", "blue").
		Pseudo(":hover", "color", "red").
		Child(".icon", "width", "16px").
		End()
	got, err := ss.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want := []string{
		`[data-fui-comp="modal"] .btn`,
		`[data-fui-comp="modal"] .btn:hover`,
		`[data-fui-comp="modal"] .btn .icon`,
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q: %s", w, got)
		}
	}
}

func TestComponentSheetMediaScopesInner(t *testing.T) {
	ss := NewComponentSheet("modal", DefaultTheme())
	ss.Rule(".body").Set("padding", "8px").
		Media("(min-width: 768px)", func(inner *ComponentSheet) {
			inner.Rule(".body").Set("padding", "16px").End()
		}).
		End()
	got, err := ss.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(got, "@media (min-width: 768px)") {
		t.Errorf("@media missing: %s", got)
	}
	// Inner rule should be scoped too.
	if !strings.Contains(got, `[data-fui-comp="modal"] .body`) {
		t.Errorf("inner .body not scoped: %s", got)
	}
}

func TestComponentSheetKeyframesUnprefixed(t *testing.T) {
	ss := NewComponentSheet("toast", DefaultTheme())
	ss.Keyframes("fade-in",
		Step("0%", "opacity", "0"),
		Step("100%", "opacity", "1"),
	)
	got, err := ss.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(got, "@keyframes fade-in") {
		t.Errorf("@keyframes missing: %s", got)
	}
	// Step selectors must not be prefixed.
	if strings.Contains(got, `[data-fui-comp="toast"] 0%`) {
		t.Errorf("keyframe step accidentally scoped: %s", got)
	}
}

func TestComponentSheetAmpersandRefersToMarkerElement(t *testing.T) {
	ss := NewComponentSheet("modal", DefaultTheme())
	ss.Rule("&").Set("display", "flex").End()
	ss.Rule("&.open").Set("opacity", "1").End()
	ss.Rule("& .header").Set("font-weight", "700").End()
	got, err := ss.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	wants := []string{
		`[data-fui-comp="modal"] {`,                       // & alone
		`[data-fui-comp="modal"].open`,                    // & combined
		`[data-fui-comp="modal"] .header`,                 // & descendant
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q:\n%s", w, got)
		}
	}
}

func TestComponentSheetRejectsUnscopableSelectors(t *testing.T) {
	cases := []string{"body", "html", ":root", "*", "::backdrop", "::view-transition-old(*)"}
	for _, sel := range cases {
		t.Run(sel, func(t *testing.T) {
			ss := NewComponentSheet("modal", DefaultTheme())
			ss.Rule(sel).Set("color", "red").End()
			if _, err := ss.Build(); err == nil {
				t.Fatalf("expected error scoping %q", sel)
			}
		})
	}
}

func TestComponentSheetMustBuildPanicMessage(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		msg, ok := r.(error)
		if !ok {
			t.Fatalf("expected error panic, got %T", r)
		}
		if !strings.Contains(msg.Error(), "theme.css") {
			t.Errorf("panic message should point to theme.css: %v", msg)
		}
	}()
	ss := NewComponentSheet("modal", DefaultTheme())
	ss.Rule("body").Set("margin", "0").End()
	_ = ss.MustBuild()
}

func TestComponentSheetDeterministic(t *testing.T) {
	build := func() string {
		ss := NewComponentSheet("modal", DefaultTheme())
		ss.Rule(".header").Set("font-weight", "700").End()
		ss.Rule(".body").Set("padding", "{spacing.lg}").End()
		ss.Rule(".footer").Set("border-top", "1px solid {colors.border}").End()
		return ss.MustBuild()
	}
	first := build()
	for i := 0; i < 100; i++ {
		got := build()
		if got != first {
			t.Fatalf("non-deterministic at iter %d:\n--- first ---\n%s\n--- got ---\n%s", i, first, got)
		}
	}
}

func TestCSSCustomPropertiesDeterministic(t *testing.T) {
	first := DefaultTheme().CSSCustomProperties()
	for i := 0; i < 100; i++ {
		got := DefaultTheme().CSSCustomProperties()
		if got != first {
			t.Fatalf("non-deterministic at iter %d", i)
		}
	}
}

func TestSplitTopLevelCommas(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{".a, .b", []string{".a", " .b"}},
		{".a", []string{".a"}},
		{`:is(.x, .y), .z`, []string{`:is(.x, .y)`, ` .z`}},
		{`[data-x="a,b"], .c`, []string{`[data-x="a,b"]`, ` .c`}},
	}
	for _, tc := range cases {
		got := splitTopLevelCommas(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("%q: got %d parts, want %d (%v)", tc.in, len(got), len(tc.want), got)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%q[%d]: got %q want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}
