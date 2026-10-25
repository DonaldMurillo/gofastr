package names

import (
	"slices"
	"testing"
)

// Keep returns exactly what the runtime set reads, in order: the kit's
// namespaces only, the first note naming a name, attribute names folded
// (a prefix entry included), and nothing of an unknown kind. The table
// generator writes this list.
func TestKeep(t *testing.T) {
	button := &Note{Version: "v9.0.0", Change: "the ui-button class is gone"}
	form := &Note{Version: "v9.1.0", Change: "the ui-form family is renamed"}
	generic := &Note{Version: "v9.1.0", Change: "the kit stopped emitting two generic names"}
	in := []Entry{
		{Kind: "class", Name: "ui-button", Note: button},
		{Kind: "attr", Name: "data-fui-signal", Note: button},
		{Kind: "attr", Name: "data-fui-toggle-", Note: button},
		{Kind: "class", Name: "ui-hero", Note: button},
		{Kind: "class", Name: "ui-form", Note: form},
		{Kind: "class", Name: "ui-button", Note: form},
		{Kind: "attr", Name: "DATA-FUI-TOGGLE-", Note: form},
		{Kind: "class", Name: "card", Note: generic},
		{Kind: "class", Name: "fui-old", Note: generic},
		{Kind: "attr", Name: "data-placeholder", Note: generic},
		{Kind: "attr", Name: "data-when-name", Note: generic},
		{Kind: "attr", Name: "Data-Hui-Old", Note: generic},
		{Kind: "attr", Name: "data-cui-old", Note: generic},
		{Kind: "attr", Name: "DATA-FUI-SIGNAL", Note: generic},
		{Kind: "selector", Name: "ui-anything", Note: generic},
	}
	var got []string
	for _, e := range Keep(in) {
		got = append(got, e.Note.Version+" "+e.Kind+" "+e.Name)
	}
	want := []string{
		"v9.0.0 class ui-button", "v9.0.0 attr data-fui-signal", "v9.0.0 attr data-fui-toggle-",
		"v9.0.0 class ui-hero", "v9.1.0 class ui-form", "v9.1.0 class fui-old",
		"v9.1.0 attr Data-Hui-Old", "v9.1.0 attr data-cui-old",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Keep() =\n%q\nwant\n%q", got, want)
	}
	if again := Keep(Keep(in)); !slices.Equal(again, Keep(in)) {
		t.Errorf("Keep is not idempotent: %v", again)
	}
}
