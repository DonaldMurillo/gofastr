//gofastr:test-reads /framework/ui/icons/

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui/icons"
)

const testSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 12h16"/></svg>`

func TestValidateRejectsActiveOrInconsistentSVG(t *testing.T) {
	for name, source := range map[string]string{
		"script":      strings.Replace(testSVG, `<path d="M4 12h16"/>`, `<script>alert(1)</script>`, 1),
		"event":       strings.Replace(testSVG, `<path `, `<path onclick="alert(1)" `, 1),
		"style":       strings.Replace(testSVG, `<path `, `<path style="stroke:red" `, 1),
		"href":        strings.Replace(testSVG, `<path `, `<path href="https://example.com" `, 1),
		"namespace":   strings.Replace(testSVG, `<path `, `<path xmlns="https://example.com" `, 1),
		"duplicate":   strings.Replace(testSVG, `d="M4 12h16"`, `d="M4 12h16" d="M0 0"`, 1),
		"grid":        strings.Replace(testSVG, `0 0 24 24`, `0 0 32 32`, 1),
		"pen":         strings.Replace(testSVG, `stroke-width="2"`, `stroke-width="3"`, 1),
		"missing pen": strings.Replace(testSVG, ` stroke-width="2"`, "", 1),
		"empty":       strings.Replace(testSVG, `<path d="M4 12h16"/>`, "", 1),
		"two roots":   testSVG + testSVG,
		"truncated":   strings.TrimSuffix(testSVG, "</svg>"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := validate([]byte(source)); err == nil {
				t.Fatal("invalid SVG was accepted")
			}
		})
	}
}

func TestValidateProducesGeometryWithoutWrapper(t *testing.T) {
	got, err := validate([]byte(testSVG))
	if err != nil {
		t.Fatal(err)
	}
	if got != `<path d="M4 12h16"></path>` {
		t.Fatalf("unexpected body %q", got)
	}
}

// A source edit without regeneration used to be invisible to consumers. Compare
// validated source bodies with compiled data so that mistake fails a normal test.
func TestGeneratedCatalogMatchesSources(t *testing.T) {
	manifest, _, err := loadSources("../..")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(manifest.Aliases, icons.Aliases()) {
		t.Fatal("compiled aliases differ from sources.json; regenerate the catalog")
	}
	files, err := filepath.Glob("../../svg/*.svg")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(icons.Names()) || len(files) != len(manifest.Icons) || len(files) == 0 {
		t.Fatalf("source/catalog size mismatch: %d sources, %d names", len(files), len(icons.Names()))
	}
	for _, file := range files {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		want, err := validate(source)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		name := strings.TrimSuffix(filepath.Base(file), ".svg")
		got, ok := icons.Lookup(name)
		if !ok || got != want {
			t.Errorf("%s differs from compiled data; run go generate ./framework/ui/icons", file)
		}
	}
}
