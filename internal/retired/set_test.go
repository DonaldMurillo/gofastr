package retired

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

const fixtureRegistry = `
through: v9.9.9
releases:
  - version: v9.0.0
    title: classes go
    notes:
      - change: "the ui-button class is gone"
        breaking: true
        hits: edit
        find:
          strings:
            classes: [ui-button]
            attrs: [data-fui-signal, data-fui-toggle-]
          css:
            classes: [ui-hero]
      - change: "a non-breaking note adds nothing"
        find:
          strings:
            classes: [ui-kept]
  - version: v9.1.0
    title: more
    notes:
      - change: "the ui-form family is renamed"
        breaking: true
        hits: edit
        find:
          strings:
            classes: [ui-form]
      - change: "the kit stopped emitting two generic names"
        breaking: true
        hits: edit
        find:
          strings:
            classes: [card, ui-form-old]
            attrs: [data-placeholder, data-when-name, data-hui-old]
`

func fixtureSet(t *testing.T) *Set {
	t.Helper()
	reg, err := upgrade.Parse(fixtureRegistry)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return FromRegistry(reg)
}

func TestSetFromRegistryBreakingOnly(t *testing.T) {
	set := fixtureSet(t)
	for name := range map[string]bool{"ui-button": true, "ui-hero": true, "ui-form": true} {
		if set.matchClass(name) == nil {
			t.Errorf("breaking class %q missing from the set", name)
		}
	}
	if set.matchClass("ui-kept") != nil {
		t.Error("non-breaking note's class leaked into the set")
	}
	if set.matchAttr([]byte("data-fui-signal")) == nil {
		t.Error("retired attr data-fui-signal missing")
	}
}

// Only the kit's own namespaces reach the runtime set: an app may own a
// "card" class or a data-placeholder attribute, and the kit no longer
// emitting one says nothing about the app's markup. The source scan
// still reports them.
func TestSetKitNamespacesOnly(t *testing.T) {
	set := fixtureSet(t)
	for _, name := range []string{"card"} {
		if set.matchClass(name) != nil {
			t.Errorf("generic class %q reached the runtime set", name)
		}
	}
	if set.matchClass("ui-form-old") == nil {
		t.Error("kit class ui-form-old missing")
	}
	for _, name := range []string{"data-placeholder", "data-when-name"} {
		if set.matchAttr([]byte(name)) != nil {
			t.Errorf("generic attr %q reached the runtime set", name)
		}
	}
	if set.matchAttr([]byte("data-hui-old")) == nil {
		t.Error("kit attr data-hui-old missing")
	}
}

func TestSetClassMatching(t *testing.T) {
	set := fixtureSet(t)
	cases := []struct {
		token string
		want  string // note change line, "" for no match
	}{
		{"ui-button", "the ui-button class is gone"},
		{"ui-button--lg", "the ui-button class is gone"},
		{"ui-button__icon", "the ui-button class is gone"},
		{"ui-button__icon--active", "the ui-button class is gone"},
		{"fui-button", ""},
		{"xui-button", ""},
		{"ui-buttonx", ""},
		{"button", ""},
		{"ui-form", "the ui-form family is renamed"},
	}
	for _, tc := range cases {
		note := set.matchClass(tc.token)
		if tc.want == "" {
			if note != nil {
				t.Errorf("class %q matched %q", tc.token, note.Change)
			}
			continue
		}
		if note == nil || note.Change != tc.want {
			t.Errorf("class %q matched %+v, want note %q", tc.token, note, tc.want)
		}
	}
}

func TestSetAttrMatching(t *testing.T) {
	set := fixtureSet(t)
	// Exact, case-insensitive (HTML attribute names fold).
	for _, a := range []string{"data-fui-signal", "DATA-FUI-SIGNAL", "Data-Fui-Signal"} {
		if set.matchAttr([]byte(a)) == nil {
			t.Errorf("attr %q did not match", a)
		}
	}
	// Prefix form: an entry ending in "-" matches extensions.
	for _, a := range []string{"data-fui-toggle-open", "data-fui-toggle-"} {
		if set.matchAttr([]byte(a)) == nil {
			t.Errorf("attr %q did not match the prefix entry", a)
		}
	}
	for _, a := range []string{"data-fui-signal-x", "data-fui-togglex", "data-fui-comp", "data-hui-signal"} {
		if set.matchAttr([]byte(a)) != nil {
			t.Errorf("attr %q matched but should not", a)
		}
	}
}

func TestSetCheck(t *testing.T) {
	set := fixtureSet(t)
	findings := set.Check([]byte(`<div class="fui-hero" data-fui-comp="ui-sidebar">
<span class='ui-button--lg x' DATA-FUI-SIGNAL>y</span>
<script>var a = '<b class="ui-button">'</script>`))
	want := []string{
		`retired markup: class "ui-button--lg" (v9.0.0: the ui-button class is gone); run gofastr upgrade`,
		`retired markup: attr "DATA-FUI-SIGNAL" (v9.0.0: the ui-button class is gone); run gofastr upgrade`,
	}
	if len(findings) != len(want) {
		t.Fatalf("findings %v, want %v", findings, want)
	}
	for i, f := range findings {
		if got := f.Message(); got != want[i] {
			t.Errorf("finding %d = %s, want %s", i, got, want[i])
		}
	}
}

// The same retired spelling on many elements is one finding per
// response; a different BEM spelling is its own finding (it is its own
// rename).
func TestSetCheckDedupes(t *testing.T) {
	set := fixtureSet(t)
	findings := set.Check([]byte(`<i class="ui-button"></i><i class="ui-button"></i><i class="ui-button"></i><i class="ui-button--sm"></i>`))
	if len(findings) != 2 {
		t.Fatalf("want 2 deduped findings, got %v", findings)
	}
	if findings[0].Name != "ui-button" || findings[1].Name != "ui-button--sm" {
		t.Fatalf("findings = %v", findings)
	}
}

func TestSetCheckEmpty(t *testing.T) {
	var set Set
	if got := set.Check([]byte(`<div class="anything" data-fui-whatever>`)); got != nil {
		t.Fatalf("empty set found %v", got)
	}
}

// Current loads the embedded registry once and caches it: the load
// counter proves both the caching and the "only when scanned" contract.
func TestCurrentLoadsOnce(t *testing.T) {
	ResetForTest(t)
	if Loads() != 0 {
		t.Fatalf("loads = %d after reset", Loads())
	}
	first := Current()
	if first == nil {
		t.Fatal("Current() = nil")
	}
	if Loads() != 1 {
		t.Fatalf("loads = %d after first Current()", Loads())
	}
	Current()
	if Loads() != 1 {
		t.Fatalf("loads = %d after second Current()", Loads())
	}
}

// A reset taken while nothing is loaded must leave the next caller a
// real load, not an empty set: an empty set would turn every later
// harness check into a silent pass.
func TestResetRestoresColdLoad(t *testing.T) {
	ResetForTest(t)
	t.Run("inner", func(t *testing.T) { ResetForTest(t) })
	if !Current().nonEmpty {
		t.Fatal("Current() is empty after an inner reset ended")
	}
}
