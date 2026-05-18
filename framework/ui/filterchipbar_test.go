package ui

import (
	"strings"
	"testing"
)

func TestFilterChipBarRendersChips(t *testing.T) {
	out := string(FilterChipBar(FilterChipBarConfig{
		Filters: []FilterChip{
			{Label: "Status: Active", DismissPath: "/filters/status/clear"},
			{Label: "Tag: urgent", DismissPath: "/filters/tag/urgent/clear", Variant: StatusWarning},
		},
	}))
	wants := []string{
		`role="toolbar"`,
		`aria-label="Active filters"`,
		`Status: Active`,
		`Tag: urgent`,
		`data-fui-rpc="/filters/status/clear"`,
		`data-fui-rpc="/filters/tag/urgent/clear"`,
		`aria-label="Remove filter Status: Active"`,
		`aria-label="Remove filter Tag: urgent"`,
		`ui-tag--warning`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("FilterChipBar missing %q\nout: %s", w, out)
		}
	}
}

func TestFilterChipBarEmpty(t *testing.T) {
	out := string(FilterChipBar(FilterChipBarConfig{}))
	if !strings.Contains(out, `role="toolbar"`) {
		t.Errorf("expected empty toolbar still rendered, got: %s", out)
	}
	if strings.Contains(out, "ui-tag") {
		t.Errorf("did not expect any chips when empty, got: %s", out)
	}
}

func TestFilterChipBarClearAll(t *testing.T) {
	out := string(FilterChipBar(FilterChipBarConfig{
		Filters: []FilterChip{
			{Label: "x", DismissPath: "/x/clear"},
		},
		ClearAllPath:  "/filters/clear-all",
		ClearAllLabel: "Reset filters",
	}))
	wants := []string{
		`data-fui-rpc="/filters/clear-all"`,
		`>Reset filters</button>`,
		`ui-filter-bar__clear`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("FilterChipBar Clear all missing %q\nout: %s", w, out)
		}
	}
}

func TestFilterChipBarClearAllOmittedWhenEmpty(t *testing.T) {
	out := string(FilterChipBar(FilterChipBarConfig{
		ClearAllPath: "/filters/clear-all",
	}))
	if strings.Contains(out, "/filters/clear-all") {
		t.Errorf("Clear all should not render with zero filters, got: %s", out)
	}
}

func TestFilterChipBarRPCSignal(t *testing.T) {
	out := string(FilterChipBar(FilterChipBarConfig{
		Filters: []FilterChip{
			{Label: "x", DismissPath: "/x/clear"},
		},
		RPCSignal:    "filter-bar",
		SignalName:   "filter-bar",
		ClearAllPath: "/clear",
	}))
	for _, w := range []string{
		`data-fui-rpc-signal="filter-bar"`,
		`data-fui-signal="filter-bar"`,
		`data-fui-signal-mode="html"`,
	} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in: %s", w, out)
		}
	}
}

func TestFilterChipBarPanicsOnInvalidChip(t *testing.T) {
	cases := []FilterChip{
		{Label: "", DismissPath: "/x"},
		{Label: "x", DismissPath: ""},
	}
	for i, c := range cases {
		t.Run("case-"+itoaSmall(i), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected panic for invalid chip")
				}
			}()
			FilterChipBar(FilterChipBarConfig{Filters: []FilterChip{c}})
		})
	}
}

func TestFilterChipBarCustomLabel(t *testing.T) {
	out := string(FilterChipBar(FilterChipBarConfig{
		Filters: []FilterChip{{Label: "x", DismissPath: "/x"}},
		Label:   "Search filters",
	}))
	if !strings.Contains(out, `aria-label="Search filters"`) {
		t.Errorf("expected custom Label, got: %s", out)
	}
}
