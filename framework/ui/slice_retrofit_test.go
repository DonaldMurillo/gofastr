package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/store"
)

// The retrofit must keep the string-name overload working AND add a
// typed *Slice overload that derives the name and stamps the declared
// default (fixing the old hardcoded "0"/"false" double-source).

func TestCounterSliceOverload(t *testing.T) {
	s := store.New("retro").Int("count", 5)
	html := string(Counter(CounterConfig{Slice: s}))
	if !strings.Contains(html, `data-fui-signal="retro.count"`) {
		t.Errorf("counter slice name not used: %s", html)
	}
	if !strings.Contains(html, `>5<`) {
		t.Errorf("counter did not stamp the slice default (5): %s", html)
	}
	if !strings.Contains(html, `data-fui-signal-inc="retro.count"`) {
		t.Errorf("increment not wired to slice name: %s", html)
	}
}

func TestCounterStringOverloadUnchanged(t *testing.T) {
	html := string(Counter(CounterConfig{SignalName: "qty"}))
	if !strings.Contains(html, `data-fui-signal="qty"`) || !strings.Contains(html, `>0<`) {
		t.Errorf("string overload regressed: %s", html)
	}
}

func TestCounterRequiresNameOrSlice(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic with neither SignalName nor Slice")
		}
	}()
	Counter(CounterConfig{})
}

func TestTabsSliceOverloadActiveIndex(t *testing.T) {
	s := store.New("retro").Int("activeTab", 1)
	html := string(Tabs(TabsConfig{Slice: s, Tabs: []TabItem{{Label: "A"}, {Label: "B"}}}))
	if !strings.Contains(html, `data-fui-signal="retro.activeTab"`) {
		t.Errorf("tabs slice name not used: %s", html)
	}
	if !strings.Contains(html, `data-active="1"`) {
		t.Errorf("tabs did not honor slice default active index 1: %s", html)
	}
	// aria-selected="true" should be on the second button (index 1).
	if strings.Count(html, `aria-selected="true"`) != 1 {
		t.Errorf("exactly one tab should be selected: %s", html)
	}
}

func TestSignalToggleSliceOverloadDefaultTrue(t *testing.T) {
	s := store.New("retro").Bool("dark", true)
	html := string(SignalToggle(SignalToggleConfig{Slice: s}))
	if !strings.Contains(html, `data-fui-signal-toggle="retro.dark"`) {
		t.Errorf("toggle slice name not used: %s", html)
	}
	if !strings.Contains(html, `aria-checked="true"`) {
		t.Errorf("toggle did not stamp slice default true: %s", html)
	}
	if !strings.Contains(html, `>true</span>`) {
		t.Errorf("toggle label did not stamp default true: %s", html)
	}
}
