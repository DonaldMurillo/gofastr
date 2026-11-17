package main

import (
	"reflect"
	"testing"
	"time"
)

func TestRecentEventsFollowCalendar(t *testing.T) {
	events := recentEvents(1000)
	if len(events) < 7 {
		t.Fatalf("only %d events; need enough to prove the feed limit", len(events))
	}
	var previous time.Time
	lastDated := ""
	undated := false
	for index, event := range events {
		if event.On == "" {
			undated = true
			continue
		}
		if undated {
			t.Errorf("dated event %q follows an undated event", event.On)
		}
		date, err := time.Parse("2006 02 Jan", "2000 "+event.On)
		if err != nil {
			t.Fatalf("invalid fixture date %q: %v", event.On, err)
		}
		if index > 0 && date.After(previous) {
			t.Errorf("%s follows older %s", event.On, previous.Format("02 Jan"))
		}
		previous = date
		lastDated = event.On
	}
	if events[0].On != "24 Sep" || lastDated != "30 Aug" {
		t.Errorf("dated feed spans %s to %s; want 24 Sep to 30 Aug", events[0].On, lastDated)
	}
	if top := recentEvents(6); !reflect.DeepEqual(top, events[:6]) {
		t.Errorf("top six = %#v; want the six newest events", top)
	}
	if top := recentEvents(0); len(top) != 0 {
		t.Errorf("zero limit returned %d events", len(top))
	}
}
