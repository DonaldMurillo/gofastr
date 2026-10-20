package main

import (
	"os"
	"testing"
)

// TestEmbeddedAppSpecsMatchSource guards against drift: the committed copy
// under exampleappspecs/ must be byte-identical to the canonical app spec.
// If you edit examples/<app>/gofastr.yml, re-copy it into exampleappspecs/
// or this test fails. (go:embed can't reach the sibling examples/ dir, so the
// copy is unavoidable; this test makes "exact yaml" enforceable.)
//
//gofastr:test-reads examples/meridian/gofastr.yml
func TestEmbeddedAppSpecsMatchSource(t *testing.T) {
	cases := []struct {
		name string
		src  string // relative to this package (examples/site)
		emb  string
	}{
		{"meridian", "../meridian/gofastr.yml", meridianAppSpecYAML},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := os.ReadFile(c.src)
			if err != nil {
				t.Fatalf("read source %s: %v", c.src, err)
			}
			if string(got) != c.emb {
				t.Errorf("embedded %q app spec has drifted from %s — re-copy the file into exampleappspecs/", c.name, c.src)
			}
		})
	}
}
