package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// catalogAppSpec is a one-screen app whose title and heading the
// generator emits as phrases.
const catalogAppSpec = `app:
  name: Shop
  module: example.com/addtest
  db:
    driver: sqlite
    url: file:test.db
entities:
  - name: posts
    fields:
      - name: title
        type: string
screens:
  - name: about
    route: /about
    title: About us
    body:
      - type: heading
        level: 1
        text: Welcome back
`

// catalogProbe runs inside the generated app: it loads the embedded
// catalog the way translatorOption does and renders the screen for a
// Spanish request.
const catalogProbe = `package main

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func TestCatalogProbe(t *testing.T) {
	c, err := i18n.LoadJSONCatalog(localeFS, "locales")
	if err != nil {
		t.Fatal(err)
	}
	tr := i18n.NewTranslator(c, "en")
	ctx := i18n.WithContext(i18n.WithTranslator(context.Background(), tr), i18n.Locale{Tag: "es"})
	s := &AboutScreen{}
	if got := string(s.RenderCtx(ctx)); !strings.Contains(got, "Bienvenido de nuevo") || strings.Contains(got, "Welcome back") {
		t.Errorf("es body = %s", got)
	}
	if got := s.ScreenTitle(ctx); got != "Sobre nosotros" {
		t.Errorf("es title = %q", got)
	}
	if got := string(s.RenderCtx(context.Background())); !strings.Contains(got, "Welcome back") {
		t.Errorf("default body lost its English: %s", got)
	}
}
`

func TestGeneratedAppSpeaksItsCatalog(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a generated app")
	}
	dir, _ := generateBaseProject(t, catalogAppSpec)
	es := `{"shop":{"screens":{"about":{"welcome_back":"Bienvenido de nuevo","about_us":"Sobre nosotros"}}}}`
	if err := os.WriteFile(filepath.Join(dir, "locales", "es.json"), []byte(es), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "catalog_probe_test.go"), []byte(catalogProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-mod=mod", "-run", "TestCatalogProbe", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated app did not speak its catalog: %v\n%s", err, out)
	}
}
