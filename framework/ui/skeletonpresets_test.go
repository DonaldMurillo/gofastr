package ui

import (
	"strings"
	"testing"
)

func TestSkeletonCard_RendersCardWithSkeletonLines(t *testing.T) {
	got := string(SkeletonCard(SkeletonCardConfig{}))
	checks := []string{
		"ui-card",                // wrapped in a Card surface
		"ui-skeleton-card",       // preset-specific class
		"skeleton skeleton-line", // title line
		"skeleton-stack",         // multi-line body stack
		`aria-hidden="true"`,     // visual-only
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("SkeletonCard output missing %q\nGOT: %s", want, got)
		}
	}
}

func TestSkeletonCard_RespectsCustomClass(t *testing.T) {
	got := string(SkeletonCard(SkeletonCardConfig{Class: "my-custom"}))
	if !strings.Contains(got, "my-custom") {
		t.Errorf("expected custom class in output, got: %s", got)
	}
}

func TestSkeletonCard_BodyLines(t *testing.T) {
	// Default is 2 body lines; explicit BodyLines overrides.
	got := string(SkeletonCard(SkeletonCardConfig{BodyLines: 4}))
	// "skeleton-line" appears once per line + once for title. With 4 body lines
	// + 1 title = 5 occurrences.
	count := strings.Count(got, "skeleton-line")
	if count < 5 {
		t.Errorf("expected >=5 skeleton-line occurrences with BodyLines=4, got %d\n%s", count, got)
	}
}

func TestSkeletonCard_FooterOptional(t *testing.T) {
	withFooter := string(SkeletonCard(SkeletonCardConfig{ShowFooter: true}))
	withoutFooter := string(SkeletonCard(SkeletonCardConfig{}))

	if !strings.Contains(withFooter, "ui-skeleton-card__footer") {
		t.Errorf("expected footer block when ShowFooter=true, got: %s", withFooter)
	}
	if strings.Contains(withoutFooter, "ui-skeleton-card__footer") {
		t.Errorf("did not expect footer block when ShowFooter=false, got: %s", withoutFooter)
	}
}

func TestSkeletonRow_RendersLabelValueChevron(t *testing.T) {
	got := string(SkeletonRow(SkeletonRowConfig{}))
	checks := []string{
		"ui-skeleton-row",
		"skeleton skeleton-line", // label + value
		"ui-skeleton-row__chevron",
		`aria-hidden="true"`,
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("SkeletonRow missing %q\nGOT: %s", want, got)
		}
	}
}

func TestSkeletonRow_NoChevron(t *testing.T) {
	got := string(SkeletonRow(SkeletonRowConfig{HideChevron: true}))
	if strings.Contains(got, "ui-skeleton-row__chevron") {
		t.Errorf("expected no chevron when HideChevron=true, got: %s", got)
	}
}

func TestSkeletonAvatar_RendersCircleAndLines(t *testing.T) {
	got := string(SkeletonAvatar(SkeletonAvatarConfig{}))
	checks := []string{
		"ui-skeleton-avatar",
		"skeleton skeleton-circle",
		"skeleton skeleton-line", // at least one text line
		`aria-hidden="true"`,
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("SkeletonAvatar missing %q\nGOT: %s", want, got)
		}
	}
}

func TestSkeletonAvatar_CustomSize(t *testing.T) {
	got := string(SkeletonAvatar(SkeletonAvatarConfig{Size: "4rem"}))
	// Circle should pick up the explicit size via inline-size:4rem.
	if !strings.Contains(got, "4rem") {
		t.Errorf("expected custom size 4rem in output, got: %s", got)
	}
}

func TestSkeletonPresets_AllRegisterCSS(t *testing.T) {
	// The shared style handle must include classes for all three presets.
	css := skeletonPresetsCSS
	for _, cls := range []string{
		".ui-skeleton-card",
		".ui-skeleton-card__footer",
		".ui-skeleton-row",
		".ui-skeleton-row__chevron",
		".ui-skeleton-avatar",
	} {
		if !strings.Contains(css, cls) {
			t.Errorf("skeletonPresetsCSS missing rule for %s", cls)
		}
	}
}
