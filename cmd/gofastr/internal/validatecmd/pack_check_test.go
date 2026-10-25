package validatecmd

import (
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestValidateFetchesNoFonts(t *testing.T) {
	stubFontFetcher(t, func(family string) ([]byte, error) {
		t.Errorf("validate fetched %q", family)
		return nil, nil
	})
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	clitest.WriteFileAll(t, path, `app:
  name: Fonts
  module: example.com/fonts
  theme:
    font_heading: Test Sans
`)
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunValidate([]string{path}) })
	})
	if code != -1 && code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}
