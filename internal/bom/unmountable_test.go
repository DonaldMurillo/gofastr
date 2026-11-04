package bom_test

import (
	"go/build"
	"path/filepath"
	"strings"
	"testing"
)

//gofastr:test-reads /core/dotenv/ /core/markdown/ /core/yaml/

// TestUnmountableReadersSkipBOMPackage holds the readers that
// `gofastr unmount` can copy out of the module to their own BOM trim.
// An unmounted copy cannot import a gofastr internal package, so these
// spell the one-line trim inline and TestReadersIgnoreBOM checks they
// agree with Trim.
func TestUnmountableReadersSkipBOMPackage(t *testing.T) {
	for _, dir := range []string{"core/dotenv", "core/markdown", "core/yaml"} {
		pkg, err := build.ImportDir(filepath.Join("..", "..", dir), 0)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		for _, imp := range pkg.Imports {
			if strings.HasPrefix(imp, "github.com/DonaldMurillo/gofastr/internal/") {
				t.Errorf("%s imports %s: an unmounted copy would not build", dir, imp)
			}
		}
	}
}
