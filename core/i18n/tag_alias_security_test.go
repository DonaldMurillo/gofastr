package i18n

import (
	"reflect"
	"runtime/debug"
	"testing"
)

// A header may name an unspecified language with an inferred base.
// Only an explicit canonical alias can extend the catalog chain.
func TestUnspecifiedTagsKeepPlainChain(t *testing.T) {
	previous := debug.SetMaxStack(64 << 10)
	defer debug.SetMaxStack(previous)
	for _, tc := range []struct {
		tag  string
		want []string
	}{
		{"und", []string{"und"}},
		{"und-US", []string{"und-us", "und"}},
		{"und-UK", []string{"und-uk", "und"}},
		{"und-Hebr", []string{"und-hebr", "und"}},
		{"x-private", []string{"x-private", "x"}},
	} {
		if got := tagFallbacks(tc.tag); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.tag, got, tc.want)
		}
	}
}
