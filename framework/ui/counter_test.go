package ui

import (
	"strconv"
	"strings"
	"testing"
)

func TestCounterBasic(t *testing.T) {
	result := Counter(CounterConfig{SignalName: "qty"})
	s := string(result)
	if !strings.Contains(s, `data-fui-signal-inc="qty"`) {
		t.Fatalf("increment button missing: %s", s)
	}
	if !strings.Contains(s, `data-fui-signal-inc="qty:-1"`) {
		t.Fatalf("decrement button missing: %s", s)
	}
	if !strings.Contains(s, `data-fui-signal="qty"`) {
		t.Fatalf("display span missing: %s", s)
	}
}

func TestCounterWithStep(t *testing.T) {
	result := Counter(CounterConfig{SignalName: "score", Step: 5})
	s := string(result)
	if !strings.Contains(s, `data-fui-signal-inc="score:5"`) {
		t.Fatalf("increment should use step 5: %s", s)
	}
	if !strings.Contains(s, `data-fui-signal-inc="score:-5"`) {
		t.Fatalf("decrement should use step -5: %s", s)
	}
}

func TestCounterRendersInitialZero(t *testing.T) {
	result := Counter(CounterConfig{SignalName: "count"})
	s := string(result)
	if !strings.Contains(s, ">0<") {
		t.Fatalf("initial value should be 0: %s", s)
	}
}

func TestCounterMissingSignalName(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for empty SignalName")
		}
		if !strings.Contains(strconv.Quote(string(resultFromPanic(r))), "SignalName") {
			t.Fatalf("panic should mention SignalName: %v", r)
		}
	}()
	Counter(CounterConfig{})
}

// Helper to extract string from panic value.
func resultFromPanic(r interface{}) string {
	if s, ok := r.(string); ok {
		return s
	}
	return ""
}
