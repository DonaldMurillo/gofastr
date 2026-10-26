package hosttest

import (
	"context"
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	_ "github.com/DonaldMurillo/gofastr/framework/localentity"
)

// sayCall is a runtime string in shipped JS: say('key', 'English' with
// either quote, as the kernel's say or NS.say.
var sayCall = regexp.MustCompile(`\bsay\(\s*(['"])([a-z][\w.]*)(['"])\s*,\s*(['"])((?:\\.|[^\\'"])*)(['"])`)

// TestRuntimeSayCallsMatchPhrases holds every say() call in the repo's
// JS to a phrase registered with runtime.RegisterStrings under the same
// key and with the same English, and every registered phrase to a call:
// the inline English is what an untranslated page shows, so it cannot
// drift from the catalog's source text, and a key the host never ships
// would never translate.
//
//gofastr:test-reads **/*.js
func TestRuntimeSayCallsMatchPhrases(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	reg := runtime.RegisteredStrings()
	said := map[string]bool{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "testdata", "dist", ".gofastr", "examples", "evals":
				return filepath.SkipDir
			}
			return nil
		}
		// runtime.js is composed from frag/*.js, which the walk reads.
		if !strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".min.js") || strings.HasSuffix(path, "core-ui/runtime/runtime.js") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, m := range sayCall.FindAllStringSubmatch(string(src), -1) {
			key, en := m[2], m[5]
			if m[1] != m[3] || m[4] != m[6] {
				t.Errorf("%s: say(%s): mismatched quotes", rel, key)
				continue
			}
			if strings.Contains(en, `\`) {
				t.Errorf("%s: say(%q): write the English without escapes, so it reads as the phrase does", rel, key)
				continue
			}
			said[key] = true
			p, ok := reg[key]
			if !ok {
				t.Errorf("%s: say(%q) names no phrase registered with runtime.RegisterStrings", rel, key)
				continue
			}
			if p.English() != en {
				t.Errorf("%s: say(%q) English %q, phrase %q", rel, key, en, p.English())
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range slices.Sorted(maps.Keys(reg)) {
		if !said[k] {
			t.Errorf("%s is registered for the runtime but no JS says it", k)
		}
	}
}

// TestStringsJSONCarriesOnlyTranslations proves the block is empty for an
// English reader and carries a translated phrase, placeholders intact,
// for one whose catalog translates it.
func TestStringsJSONCarriesOnlyTranslations(t *testing.T) {
	if got := runtime.StringsJSON(context.Background()); got != nil {
		t.Fatalf("no translator: block = %s, want none", got)
	}
	c := i18n.NewMapCatalog()
	c.Set("es", "runtime.panel_limit", i18n.Message{Text: "{{n}} paneles es el límite"})
	c.Set("es", "runtime.network_error", i18n.Message{Text: "Network error: please try again"})
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "es"})
	ctx = i18n.WithTranslator(ctx, i18n.NewTranslator(c, "en"))
	var got map[string]string
	if err := json.Unmarshal(runtime.StringsJSON(ctx), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["runtime.panel_limit"] != "{{n}} paneles es el límite" {
		t.Fatalf("block = %v; want only the translated panel_limit, placeholder kept", got)
	}
}
