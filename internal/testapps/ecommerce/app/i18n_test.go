//gofastr:generated generator=app kind=scaffold version=dev sum=00ab1c6986580164

package main

import (
	"io/fs"
	"path"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// Each locales/<tag>.json names only phrases the app and the framework
// define, and keeps every {{placeholder}} of a phrase's English. Entity
// keys name the app's data rather than a defined phrase, so the check
// leaves them alone. Pass i18n.Require("<app>.") to also fail on a
// phrase a locale does not translate yet.
func TestCatalogsMatchPhrases(t *testing.T) {
	c, err := i18n.LoadJSONCatalog(localeFS, "locales")
	if err != nil {
		t.Fatal(err)
	}
	names, err := fs.Glob(localeFS, "locales/*.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		tag := strings.TrimSuffix(path.Base(name), ".json")
		for _, is := range i18n.CheckCatalog(c, tag, i18n.Skip("entity.")) {
			t.Errorf("%s: %s: %s (English %q, translation %q)", tag, is.Key, is.Problem, is.English, is.Text)
		}
	}
}
