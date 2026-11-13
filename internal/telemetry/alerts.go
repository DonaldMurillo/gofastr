package telemetry

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// AlertKind is what an alert rule counts.
type AlertKind string

const (
	// AlertFailed counts requests that failed.
	AlertFailed AlertKind = "failed"
	// AlertSlow counts requests at or over the slow threshold.
	AlertSlow AlertKind = "slow"
	// AlertDisk watches how full the disk is. Its rule's AtLeast is a
	// percentage, and it has no window and no route: a disk is as
	// full as it is.
	AlertDisk AlertKind = "disk"
)

// diskSlack is how far below its line, in points, a disk must fall for
// its rule to resolve: a disk sitting at the line does not fire and
// resolve by turns.
const diskSlack = 5

// AlertRule fires when at least AtLeast requests of its Kind finished
// within the last Within.
type AlertRule struct {
	// Name identifies the rule in a notification. Required, unique.
	Name string
	Kind AlertKind
	// AtLeast is the count that fires the rule. At least 1.
	AtLeast int
	// Within is the window counted, from one minute to one hour.
	// Zero means five minutes.
	Within time.Duration
	// Route, when set, counts only that route.
	Route string
}

// AlertEvent is a rule starting to fire, or stopping.
type AlertEvent struct {
	Rule AlertRule
	// Firing is true when the rule started firing and false when it
	// resolved.
	Firing bool
	// Count is the requests counted in the window at that moment; for
	// a disk rule, the percentage full.
	Count int
	// At is when the change was noticed; Since is when the rule started
	// firing.
	At, Since time.Time
}

// alertBucket is the width of one counting slot: a window's edge is
// accurate to this.
const alertBucket = 10 * time.Second

type alertState struct {
	rule    AlertRule
	counts  []int // one per bucket, a ring indexed by bucket number
	newest  int64 // the bucket number counts was last moved to
	firing  bool
	since   time.Time
	lastSum int
	// disk is the last percentage a disk rule was told, or negative
	// before the first.
	disk int
}

// Alerts counts finished requests against a set of rules, in memory:
// ten seconds of resolution, an hour of history at most, nothing on
// disk. A restart starts the counts again, which a window measured in
// minutes forgives.
type Alerts struct {
	mu     sync.Mutex
	states []*alertState
}

// NewAlerts checks the rules and returns the counter for them.
func NewAlerts(rules []AlertRule) (*Alerts, error) {
	a := &Alerts{}
	seen := map[string]bool{}
	for _, r := range rules {
		switch {
		case r.Name == "" || seen[r.Name]:
			return nil, fmt.Errorf("telemetry: alert rule name %q is empty or used twice", r.Name)
		case r.Kind != AlertFailed && r.Kind != AlertSlow && r.Kind != AlertDisk:
			return nil, fmt.Errorf("telemetry: alert rule %q: Kind must be AlertFailed, AlertSlow or AlertDisk", r.Name)
		case r.Kind == AlertDisk && (r.AtLeast < 1 || r.AtLeast > 100 || r.Within != 0 || r.Route != ""):
			return nil, fmt.Errorf("telemetry: alert rule %q: a disk rule takes AtLeast from 1 to 100 (a percentage), and no Within or Route", r.Name)
		case r.AtLeast < 1:
			return nil, fmt.Errorf("telemetry: alert rule %q: AtLeast must be at least 1", r.Name)
		case r.Within < 0:
			return nil, fmt.Errorf("telemetry: alert rule %q: Within is negative", r.Name)
		}
		if r.Kind == AlertDisk {
			seen[r.Name] = true
			a.states = append(a.states, &alertState{rule: r, disk: -1})
			continue
		}
		if r.Within == 0 {
			r.Within = 5 * time.Minute
		}
		if r.Within < time.Minute || r.Within > time.Hour {
			return nil, errors.New("telemetry: alert rule " + r.Name + ": Within must be from one minute to one hour")
		}
		seen[r.Name] = true
		a.states = append(a.states, &alertState{rule: r, counts: make([]int, int(r.Within/alertBucket))})
	}
	return a, nil
}

// advance moves a rule's ring to the bucket holding at, clearing the
// slots that time has passed.
func (s *alertState) advance(at time.Time) int64 {
	b := at.UnixNano() / int64(alertBucket)
	if b <= s.newest {
		return b
	}
	gap := b - s.newest
	if s.newest == 0 || gap >= int64(len(s.counts)) {
		clear(s.counts)
	} else {
		for i := s.newest + 1; i <= b; i++ {
			s.counts[i%int64(len(s.counts))] = 0
		}
	}
	s.newest = b
	return b
}

// Observe counts one finished request. A disk rule has no buckets, so
// a request that gets past its kind test has nowhere to be counted.
func (a *Alerts) Observe(at time.Time, route string, failed, slow bool) {
	if !failed && !slow {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.states {
		if (s.rule.Kind == AlertFailed && !failed) || (s.rule.Kind == AlertSlow && !slow) || (s.rule.Route != "" && s.rule.Route != route) {
			continue
		}
		b := s.advance(at)
		// A request stamped before the window is too old to count.
		if s.newest-b < int64(len(s.counts)) {
			s.counts[b%int64(len(s.counts))]++
		}
	}
}

// SetDisk tells the disk rules how full the disk is, 0 to 100. A
// negative percent is "unknown" and changes nothing.
func (a *Alerts) SetDisk(percent int) {
	if percent < 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.states {
		if s.rule.Kind == AlertDisk {
			s.disk = min(percent, 100)
		}
	}
}

// Check brings every rule up to now and returns the ones that changed:
// a rule fires when its count reaches AtLeast, and resolves when the
// count has fallen to half of that or less, so a rate hovering at the
// threshold does not fire and resolve by turns.
func (a *Alerts) Check(now time.Time) []AlertEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	var events []AlertEvent
	for _, s := range a.states {
		if s.rule.Kind == AlertDisk {
			// Before the first reading disk is negative, which is
			// under every line: nothing fires.
			s.lastSum = s.disk
			switch {
			case !s.firing && s.disk >= s.rule.AtLeast:
				s.firing, s.since = true, now
				events = append(events, AlertEvent{Rule: s.rule, Firing: true, Count: s.disk, At: now, Since: now})
			case s.firing && s.disk <= s.rule.AtLeast-diskSlack:
				s.firing = false
				events = append(events, AlertEvent{Rule: s.rule, Firing: false, Count: s.disk, At: now, Since: s.since})
			}
			continue
		}
		s.advance(now)
		sum := 0
		for _, n := range s.counts {
			sum += n
		}
		s.lastSum = sum
		switch {
		case !s.firing && sum >= s.rule.AtLeast:
			s.firing, s.since = true, now
			events = append(events, AlertEvent{Rule: s.rule, Firing: true, Count: sum, At: now, Since: now})
		case s.firing && sum <= s.rule.AtLeast/2:
			s.firing = false
			events = append(events, AlertEvent{Rule: s.rule, Firing: false, Count: sum, At: now, Since: s.since})
		}
	}
	return events
}

// Firing returns the rules firing as of the last Check, in the order
// they were given.
func (a *Alerts) Firing() []AlertEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []AlertEvent
	for _, s := range a.states {
		if s.firing {
			out = append(out, AlertEvent{Rule: s.rule, Firing: true, Count: s.lastSum, At: s.since, Since: s.since})
		}
	}
	return out
}
