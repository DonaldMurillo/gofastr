package retired

import (
	"slices"
	"testing"
)

// fixtureEntries is what the table generator emits for two breaking
// notes, plus the generic names a note can name that New must drop: the
// generator hands New every breaking note's names, kit namespace or not.
func fixtureEntries() []Entry {
	button := &Note{Version: "v9.0.0", Change: "the ui-button class is gone"}
	form := &Note{Version: "v9.1.0", Change: "the ui-form family is renamed"}
	generic := &Note{Version: "v9.1.0", Change: "the kit stopped emitting two generic names"}
	return []Entry{
		{Kind: "class", Name: "ui-button", Note: button},
		{Kind: "attr", Name: "data-fui-signal", Note: button},
		{Kind: "attr", Name: "data-fui-toggle-", Note: button},
		{Kind: "class", Name: "ui-hero", Note: button},
		{Kind: "class", Name: "ui-form", Note: form},
		{Kind: "class", Name: "ui-button", Note: form}, // a later mention keeps the first note
		{Kind: "class", Name: "card", Note: generic},
		{Kind: "class", Name: "ui-form-old", Note: generic},
		{Kind: "attr", Name: "data-placeholder", Note: generic},
		{Kind: "attr", Name: "data-when-name", Note: generic},
		{Kind: "attr", Name: "data-hui-old", Note: generic},
		{Kind: "attr", Name: "DATA-FUI-SIGNAL", Note: generic}, // attribute names fold: a duplicate
	}
}

func fixtureSet(t *testing.T) *Set {
	t.Helper()
	return New(fixtureEntries())
}

// The first entry naming a name wins, and Entries is exactly what the
// set kept, in order: the list the table generator writes.
func TestNewKeepsFirstNoteAndListsEntries(t *testing.T) {
	set := fixtureSet(t)
	if note := set.matchClass("ui-button"); note == nil || note.Version != "v9.0.0" {
		t.Errorf("ui-button matched %+v, want the v9.0.0 note", note)
	}
	if note := set.matchAttr([]byte("data-fui-signal")); note == nil || note.Version != "v9.0.0" {
		t.Errorf("data-fui-signal matched %+v, want the v9.0.0 note", note)
	}
	var got []string
	for _, e := range set.Entries() {
		got = append(got, e.Kind+" "+e.Name)
	}
	want := []string{
		"class ui-button", "attr data-fui-signal", "attr data-fui-toggle-", "class ui-hero",
		"class ui-form", "class ui-form-old", "attr data-hui-old",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Entries() = %q, want %q", got, want)
	}
	if again := New(set.Entries()).Entries(); !slices.Equal(again, set.Entries()) {
		t.Errorf("rebuilding from Entries() changed the set: %v", again)
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

// Current builds the set from the generated table once and caches it:
// the load counter proves both the caching and the "only when scanned"
// contract.
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
