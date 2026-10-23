package i18n

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestPluralRulesMatchCLDRSamples: every CLDR rule set picks, for each
// sample CLDR lists under a category, that category.
func TestPluralRulesMatchCLDRSamples(t *testing.T) {
	if len(cldrPluralSamples) < 200 {
		t.Fatalf("only %d rule sets in the samples table", len(cldrPluralSamples))
	}
	checked := 0
	for id, lists := range cldrPluralSamples {
		for _, cond := range pluralRulesFor(id) {
			if cond == "" {
				continue
			}
			if _, err := parsePluralCond(cond); err != nil {
				t.Errorf("%s: %v", id, err)
			}
		}
		for ci, list := range lists {
			want := "other"
			if ci < len(pluralNames) {
				want = pluralNames[ci]
			}
			for _, n := range expandSamples(t, list) {
				ops, ok := decimalOperands(n)
				if !ok {
					t.Fatalf("%s %s: sample %q is no decimal", id, want, n)
				}
				if got := pluralCategory(id, ops); got != want {
					t.Errorf("%s %s: got %q want %q", id, n, got, want)
				}
				checked++
			}
		}
	}
	if checked < 10000 {
		t.Fatalf("checked only %d samples", checked)
	}
}

// expandSamples lists the plain numbers of a CLDR sample string
// ("@integer 0, 2~16, … @decimal 0.0~1.5, …"): ranges expanded at the
// start's precision, the ellipsis and compact samples (1c6) skipped.
func expandSamples(t *testing.T, s string) []string {
	t.Helper()
	var out []string
	for _, sec := range strings.Split(s, "@") {
		_, items, _ := strings.Cut(sec, " ")
		for _, item := range strings.Split(items, ",") {
			item = strings.TrimSpace(item)
			if item == "" || item == "…" || strings.ContainsAny(item, "ce") {
				continue
			}
			lo, hi, isRange := strings.Cut(item, "~")
			if !isRange {
				out = append(out, item)
				continue
			}
			_, frac, _ := strings.Cut(lo, ".")
			scale := len(frac)
			a, err1 := strconv.Atoi(strings.ReplaceAll(lo, ".", ""))
			b, err2 := strconv.Atoi(strings.ReplaceAll(hi, ".", ""))
			if err1 != nil || err2 != nil || a > b {
				t.Fatalf("sample range %q", item)
			}
			for v := a; v <= b; v++ {
				digits := strconv.Itoa(v)
				if scale == 0 {
					out = append(out, digits)
					continue
				}
				for len(digits) <= scale {
					digits = "0" + digits
				}
				out = append(out, digits[:len(digits)-scale]+"."+digits[len(digits)-scale:])
			}
		}
	}
	return out
}

// TestFlexibleDayPeriod: zh-Hant's "B" pattern names the part of the
// day CLDR's rules put the hour in, not a bare AM/PM.
func TestFlexibleDayPeriod(t *testing.T) {
	ctx := WithContext(context.Background(), Locale{Tag: "zh-Hant"})
	for _, tc := range []struct {
		h, m int
		want string
	}{
		{0, 0, "午夜"},
		{3, 0, "凌晨"},
		{6, 30, "清晨"},
		{9, 15, "上午"},
		{12, 30, "中午"},
		{15, 4, "下午"},
		{21, 0, "晚上"},
	} {
		at := time.Date(2026, 3, 5, tc.h, tc.m, 0, 0, time.UTC)
		if got := Time(ctx, at, TimeShort); !strings.HasPrefix(got, tc.want) {
			t.Errorf("%02d:%02d: got %q, want it to start %q", tc.h, tc.m, got, tc.want)
		}
	}
}
