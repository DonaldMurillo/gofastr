package component

import (
	"slices"
	"testing"
)

func TestActionsSortedByEvent(t *testing.T) {
	registry := NewActionRegistry()
	want := []string{"change", "click", "input", "keydown", "submit"}
	for _, event := range want {
		registry.Register(ActionDef{Event: event})
	}
	for range 100 {
		var got []string
		for _, action := range registry.All() {
			got = append(got, action.Event)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("action order = %v, want %v", got, want)
		}
	}
}
