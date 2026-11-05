package main

import "testing"

func TestDoneWeekEndsAtNewestDone(t *testing.T) {
	done := func(updated string) Issue { return Issue{Status: statusDone, Updated: updated} }
	for _, test := range []struct {
		name   string
		issues []Issue
		want   int
	}{
		{"fixture", allIssues(), 3},
		{"all in one week", []Issue{done("03 Sep"), done("04 Sep"), done("05 Sep"), done("06 Sep"), done("09 Sep")}, 5},
		{"eight days back is out", []Issue{done("01 Sep"), done("09 Sep")}, 1},
		{"across a month end", []Issue{done("30 Aug"), done("02 Sep"), done("23 Aug")}, 2},
		{"open issues ignored", []Issue{done("09 Sep"), {Status: "Todo", Updated: "09 Sep"}}, 1},
		{"undated done ignored", []Issue{done("09 Sep"), done("")}, 1},
		{"none", nil, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := doneInNewestWeek(test.issues); got != test.want {
				t.Fatalf("doneInNewestWeek = %d, want %d", got, test.want)
			}
		})
	}
}
