package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticExportWritesCatalogProducts(t *testing.T) {
	fwApp := newTestApp(t)
	dir := t.TempDir()
	if err := fwApp.ExportStatic(t.Context(), dir, "/gofastr"); err != nil {
		t.Fatalf("ExportStatic: %v", err)
	}
	index, err := os.ReadFile(filepath.Join(dir, "examples", "catalog", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range catalogItems {
		t.Run(item.ID, func(t *testing.T) {
			link := "/gofastr/examples/catalog/" + item.ID + "/"
			if !strings.Contains(string(index), `href="`+link+`"`) {
				t.Errorf("catalog missing base-prefixed link %s", link)
			}
			page, err := os.ReadFile(filepath.Join(dir, strings.TrimPrefix(link, "/gofastr/"), "index.html"))
			if err != nil {
				t.Fatalf("exported product page missing: %v", err)
			}
			for _, content := range []string{item.Name, item.Price, item.Blurb} {
				if !strings.Contains(string(page), content) {
					t.Errorf("exported product page missing %q", content)
				}
			}
		})
	}
}
