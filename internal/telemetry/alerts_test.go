package telemetry

import (
	"testing"
	"time"
)

func mustAlerts(t *testing.T, rules ...AlertRule) *Alerts {
	t.Helper()
	a, err := NewAlerts(rules)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// A rule fires when its count reaches the threshold within the window,
// says so once, and resolves once the failures have aged out to half
// the threshold or less: not while they hover at it.
func TestAlertFiresOnceAndResolvesWhenTheFailuresAgeOut(t *testing.T) {
	a := mustAlerts(t, AlertRule{Name: "failures", Kind: AlertFailed, AtLeast: 10, Within: 5 * time.Minute})
	at := day0
	fail := func(n int) {
		for range n {
			a.Observe(at, "GET /x", true, false)
		}
	}
	fail(9)
	if ev := a.Check(at); len(ev) != 0 || len(a.Firing()) != 0 {
		t.Fatalf("9 failures against a threshold of 10: %+v", ev)
	}
	at = at.Add(2 * time.Minute)
	fail(1)
	ev := a.Check(at)
	if len(ev) != 1 || !ev[0].Firing || ev[0].Count != 10 || ev[0].Rule.Name != "failures" || !ev[0].Since.Equal(at) {
		t.Fatalf("the tenth failure: %+v", ev)
	}
	fired := at
	// Still failing: no second notice, and the count is current.
	at = at.Add(time.Minute)
	fail(3)
	if ev := a.Check(at); len(ev) != 0 {
		t.Fatalf("a firing rule spoke again: %+v", ev)
	}
	if f := a.Firing(); len(f) != 1 || f[0].Count != 13 || !f[0].Since.Equal(fired) {
		t.Fatalf("Firing() while firing: %+v", f)
	}
	// Five minutes after the first nine, they have aged out: 4 left,
	// which is under the threshold and under half of it.
	at = day0.Add(5*time.Minute + alertBucket)
	ev = a.Check(at)
	if len(ev) != 1 || ev[0].Firing || ev[0].Count != 4 || !ev[0].Since.Equal(fired) || len(a.Firing()) != 0 {
		t.Fatalf("after the first failures aged out: %+v", ev)
	}
	// A long quiet, then a burst: it fires again from nothing.
	at = at.Add(3 * time.Hour)
	fail(10)
	if ev := a.Check(at); len(ev) != 1 || !ev[0].Firing || ev[0].Count != 10 {
		t.Fatalf("a burst after a long quiet: %+v", ev)
	}
}

// A count that sits between half the threshold and the threshold keeps
// a firing rule firing: it does not resolve and fire by turns.
func TestAlertDoesNotFlapNearItsThreshold(t *testing.T) {
	a := mustAlerts(t, AlertRule{Name: "f", Kind: AlertFailed, AtLeast: 10, Within: time.Minute})
	at := day0
	for range 10 {
		a.Observe(at, "", true, false)
	}
	if ev := a.Check(at); len(ev) != 1 {
		t.Fatalf("did not fire: %+v", ev)
	}
	changes := 0
	for range 20 {
		// Seven failures a minute: under the threshold, over half.
		at = at.Add(time.Minute)
		for range 7 {
			a.Observe(at, "", true, false)
		}
		changes += len(a.Check(at))
	}
	if changes != 0 || len(a.Firing()) != 1 {
		t.Fatalf("%d changes while the count hovered at 7 of 10", changes)
	}
}

// A rule counts its own kind, on its own route, and nothing else.
func TestAlertRulesCountOnlyWhatTheyName(t *testing.T) {
	a := mustAlerts(t,
		AlertRule{Name: "failed", Kind: AlertFailed, AtLeast: 2},
		AlertRule{Name: "slow", Kind: AlertSlow, AtLeast: 2},
		AlertRule{Name: "checkout", Kind: AlertFailed, AtLeast: 2, Route: "POST /checkout"})
	at := day0
	a.Observe(at, "GET /a", false, false) // fine: counts nowhere
	a.Observe(at, "GET /a", true, false)
	a.Observe(at, "GET /a", false, true)
	a.Observe(at, "POST /checkout", true, true)
	got := map[string]int{}
	for _, e := range a.Check(at) {
		got[e.Rule.Name] = e.Count
	}
	if len(got) != 2 || got["failed"] != 2 || got["slow"] != 2 {
		t.Fatalf("fired: %v; want failed and slow at 2, checkout still at 1", got)
	}
	a.Observe(at, "POST /checkout", true, false)
	if ev := a.Check(at); len(ev) != 1 || ev[0].Rule.Name != "checkout" || ev[0].Count != 2 {
		t.Fatalf("the route rule: %+v", ev)
	}
	// A request stamped before the window does not count.
	b := mustAlerts(t, AlertRule{Name: "f", Kind: AlertFailed, AtLeast: 1, Within: time.Minute})
	b.Check(at)
	b.Observe(at.Add(-2*time.Minute), "", true, false)
	if ev := b.Check(at); len(ev) != 0 {
		t.Fatalf("a failure from before the window fired the rule: %+v", ev)
	}
}

func TestAlertRulesAreChecked(t *testing.T) {
	ok := AlertRule{Name: "a", Kind: AlertFailed, AtLeast: 1}
	if a, err := NewAlerts([]AlertRule{ok}); err != nil || a == nil {
		t.Fatalf("a good rule: %v", err)
	}
	for name, rules := range map[string][]AlertRule{
		"no name":        {{Kind: AlertFailed, AtLeast: 1}},
		"name twice":     {ok, ok},
		"unknown kind":   {{Name: "a", Kind: "loud", AtLeast: 1}},
		"zero threshold": {{Name: "a", Kind: AlertFailed}},
		"negative":       {{Name: "a", Kind: AlertFailed, AtLeast: 1, Within: -time.Minute}},
		"too short":      {{Name: "a", Kind: AlertFailed, AtLeast: 1, Within: time.Second}},
		"too long":       {{Name: "a", Kind: AlertFailed, AtLeast: 1, Within: 2 * time.Hour}},
	} {
		if _, err := NewAlerts(rules); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// A disk rule fires when the disk is as full as its line, resolves five
// points below it, says nothing before the first reading, and is not
// moved by requests.
func TestDiskRuleFollowsTheDisk(t *testing.T) {
	a, err := NewAlerts([]AlertRule{{Name: "disk", Kind: AlertDisk, AtLeast: 90}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	check := func(what string, percent int, wantEvent, wantFiring bool) {
		t.Helper()
		a.SetDisk(percent)
		events := a.Check(now)
		now = now.Add(15 * time.Second)
		if (len(events) == 1) != wantEvent || (len(a.Firing()) == 1) != wantFiring {
			t.Fatalf("%s (%d%%): %d events, %d firing; want event=%v firing=%v", what, percent, len(events), len(a.Firing()), wantEvent, wantFiring)
		}
		if wantEvent && (events[0].Count != min(percent, 100) || events[0].Firing != wantFiring) {
			t.Fatalf("%s: event %+v", what, events[0])
		}
	}
	if len(a.Check(now)) != 0 {
		t.Fatal("a rule fired before any reading")
	}
	// A hundred failed requests are not a full disk.
	for range 100 {
		a.Observe(now, "GET /a", true, true)
	}
	check("unknown", -1, false, false)
	check("under the line", 89, false, false)
	check("at the line", 90, true, true)
	check("still full", 97, false, true)
	check("unreadable while firing", -1, false, true)
	check("a point under", 89, false, true)
	check("just short of resolving", 86, false, true)
	check("five points under", 85, true, false)
	check("an unknown reading keeps the last", -1, false, false)
	check("past full", 250, true, true)
	if f := a.Firing(); f[0].Count != 100 {
		t.Fatalf("a disk more than full reads as %d%%", f[0].Count)
	}

	for name, r := range map[string]AlertRule{
		"no line":        {Name: "d", Kind: AlertDisk},
		"over a hundred": {Name: "d", Kind: AlertDisk, AtLeast: 101},
		"a window":       {Name: "d", Kind: AlertDisk, AtLeast: 90, Within: time.Minute},
		"a route":        {Name: "d", Kind: AlertDisk, AtLeast: 90, Route: "GET /a"},
	} {
		if _, err := NewAlerts([]AlertRule{r}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := NewAlerts([]AlertRule{{Name: "d", Kind: AlertDisk, AtLeast: 100}, {Name: "e", Kind: AlertDisk, AtLeast: 1}}); err != nil {
		t.Errorf("the two ends of the range: %v", err)
	}
}
