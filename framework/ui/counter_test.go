package ui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
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

// TestCounterRegistersCSS guards that Counter ships its own scoped CSS
// via the registry (otherwise the buttons render unstyled — the marker
// is present but no style is registered under that name).
func TestCounterRegistersCSS(t *testing.T) {
	css := counterStyle.Entry().CSSFor(style.Theme{})
	for _, sel := range []string{
		`[data-fui-comp="fui-counter"]`,
		".fui-counter__btn",
		".fui-counter__value",
	} {
		if !strings.Contains(css, sel) {
			t.Errorf("counter CSS missing %q:\n%s", sel, css)
		}
	}
}

// TestCounterCarriesCompMarker ensures the rendered output carries the
// data-fui-comp marker the host scans for to emit the registered CSS.
func TestCounterCarriesCompMarker(t *testing.T) {
	s := string(Counter(CounterConfig{SignalName: "qty"}))
	if !strings.Contains(s, `data-fui-comp="fui-counter"`) {
		t.Fatalf("counter missing comp marker: %s", s)
	}
}

// Helper to extract string from panic value.
func resultFromPanic(r interface{}) string {
	if s, ok := r.(string); ok {
		return s
	}
	return ""
}
