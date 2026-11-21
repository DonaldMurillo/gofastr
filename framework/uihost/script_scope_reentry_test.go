package uihost

import (
	"slices"
	"testing"
	"time"
)

func TestDocumentScriptScopeReentersHost(t *testing.T) {
	host := newTestUIHost()
	if err := host.RegisterDocumentScript("/doc.js", func(string) bool {
		return host.RegisterExternalScript("/nested.js") == nil
	}); err != nil {
		t.Fatal(err)
	}

	result := make(chan []string, 1)
	go func() { result <- host.ExtraScriptSrcs([]string{"/"}) }()
	select {
	case got := <-result:
		if !slices.Equal(got, []string{"/doc.js"}) {
			t.Fatalf("first snapshot = %v, want [/doc.js]", got)
		}
	case <-time.After(time.Second):
		t.Fatal("ExtraScriptSrcs blocked while its scope registered another script")
	}
	if got := host.ExtraScriptSrcs([]string{"/"}); !slices.Equal(got, []string{"/doc.js", "/nested.js"}) {
		t.Fatalf("next snapshot = %v, want [/doc.js /nested.js]", got)
	}
}
